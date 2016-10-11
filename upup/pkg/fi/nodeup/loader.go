package nodeup

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/golang/glog"
	"k8s.io/kops/upup/pkg/api"
	"k8s.io/kops/upup/pkg/fi"
	"k8s.io/kops/upup/pkg/fi/loader"
	"k8s.io/kops/upup/pkg/fi/nodeup/nodetasks"
	"k8s.io/kops/util/pkg/vfs"
	"strings"
	"text/template"
)

type Loader struct {
	templates []*template.Template
	config    *NodeUpConfig
	cluster   *api.Cluster

	assets *fi.AssetStore
	tasks  map[string]fi.Task

	tags              map[string]struct{}
	TemplateFunctions template.FuncMap
}

func NewLoader(config *NodeUpConfig, cluster *api.Cluster, assets *fi.AssetStore, tags map[string]struct{}) *Loader {
	l := &Loader{}
	l.assets = assets
	l.tasks = make(map[string]fi.Task)
	l.config = config
	l.cluster = cluster
	l.TemplateFunctions = make(template.FuncMap)
	l.tags = tags

	return l
}

func (l *Loader) executeTemplate(key string, d string) (string, error) {
	t := template.New(key)

	funcMap := make(template.FuncMap)
	for k, fn := range l.TemplateFunctions {
		funcMap[k] = fn
	}
	t.Funcs(funcMap)

	context := l.cluster.Spec

	_, err := t.Parse(d)
	if err != nil {
		return "", fmt.Errorf("error parsing template %q: %v", key, err)
	}

	t.Option("missingkey=zero")

	var buffer bytes.Buffer
	err = t.ExecuteTemplate(&buffer, key, context)
	if err != nil {
		return "", fmt.Errorf("error executing template %q: %v", key, err)
	}

	return buffer.String(), nil
}

func ignoreHandler(i *loader.TreeWalkItem) error {
	return nil
}

func (l *Loader) Build(baseDir vfs.Path) (map[string]fi.Task, error) {
	// First pass: load options
	tw := &loader.TreeWalker{
		DefaultHandler: ignoreHandler,
		Contexts: map[string]loader.Handler{
			"files":    ignoreHandler,
			"disks":    ignoreHandler,
			"packages": ignoreHandler,
			"services": ignoreHandler,
			"users":    ignoreHandler,
		},
		Tags: l.tags,
	}

	err := tw.Walk(baseDir)
	if err != nil {
		return nil, err
	}

	// Second pass: load everything else
	tw = &loader.TreeWalker{
		DefaultHandler: l.handleFile,
		Contexts: map[string]loader.Handler{
			"files":    l.handleFile,
			"disks":    l.newTaskHandler("disk/", nodetasks.NewMountDiskTask),
			"packages": l.newTaskHandler("package/", nodetasks.NewPackage),
			"services": l.newTaskHandler("service/", nodetasks.NewService),
			"users":    l.newTaskHandler("user/", nodetasks.NewUserTask),
		},
		Tags: l.tags,
	}

	err = tw.Walk(baseDir)
	if err != nil {
		return nil, err
	}

	// If there is a package task, we need an update packages task
	for _, t := range l.tasks {
		if _, ok := t.(*nodetasks.Package); ok {
			glog.Infof("Package task found; adding UpdatePackages task")
			l.tasks["UpdatePackages"] = nodetasks.NewUpdatePackages()
			break
		}
	}
	if l.tasks["UpdatePackages"] == nil {
		glog.Infof("No package task found; won't update packages")
	}

	return l.tasks, nil
}

type TaskBuilder func(name string, contents string, meta string) (fi.Task, error)

func (r *Loader) newTaskHandler(prefix string, builder TaskBuilder) loader.Handler {
	return func(i *loader.TreeWalkItem) error {
		contents, err := i.ReadString()
		if err != nil {
			return err
		}
		name := i.Name
		if strings.HasSuffix(name, ".template") {
			name = strings.TrimSuffix(name, ".template")
			expanded, err := r.executeTemplate(name, contents)
			if err != nil {
				return fmt.Errorf("error executing template %q: %v", i.RelativePath, err)
			}

			contents = expanded
		}

		task, err := builder(name, contents, i.Meta)
		if err != nil {
			return fmt.Errorf("error building %s for %q: %v", i.Name, i.Path, err)
		}
		key := prefix + i.RelativePath

		if task != nil {
			r.tasks[key] = task
		}
		return nil
	}
}

func (r *Loader) handleFile(i *loader.TreeWalkItem) error {
	var task *nodetasks.File
	defaultFileType := nodetasks.FileType_File

	var err error
	if strings.HasSuffix(i.RelativePath, ".template") {
		contents, err := i.ReadString()
		if err != nil {
			return err
		}

		// TODO: Use template resource here to defer execution?
		destPath := "/" + strings.TrimSuffix(i.RelativePath, ".template")
		name := strings.TrimSuffix(i.Name, ".template")
		expanded, err := r.executeTemplate(name, contents)
		if err != nil {
			return fmt.Errorf("error executing template %q: %v", i.RelativePath, err)
		}

		task, err = nodetasks.NewFileTask(name, fi.NewStringResource(expanded), destPath, i.Meta)
	} else if strings.HasSuffix(i.RelativePath, ".asset") {
		contents, err := i.ReadBytes()
		if err != nil {
			return err
		}

		destPath := "/" + strings.TrimSuffix(i.RelativePath, ".asset")
		name := strings.TrimSuffix(i.Name, ".asset")

		def := &nodetasks.AssetDefinition{}
		err = json.Unmarshal(contents, def)
		if err != nil {
			return fmt.Errorf("error parsing json for asset %q: %v", name, err)
		}

		asset, err := r.assets.Find(name, def.AssetPath)
		if err != nil {
			return fmt.Errorf("error trying to locate asset %q: %v", name, err)
		}
		if asset == nil {
			return fmt.Errorf("unable to locate asset %q", name)
		}

		task, err = nodetasks.NewFileTask(i.Name, asset, destPath, i.Meta)
	} else {
		var contents fi.Resource
		if vfs.IsDirectory(i.Path) {
			defaultFileType = nodetasks.FileType_Directory
		} else {
			contents = fi.NewVFSResource(i.Path)
		}
		task, err = nodetasks.NewFileTask(i.Name, contents, "/"+i.RelativePath, i.Meta)
	}

	if task.Type == "" {
		task.Type = defaultFileType
	}

	if err != nil {
		return fmt.Errorf("error building task %q: %v", i.RelativePath, err)
	}
	glog.V(2).Infof("path %q -> task %v", i.Path, task)

	if task != nil {
		key := "file/" + i.RelativePath
		r.tasks[key] = task
	}
	return nil
}
