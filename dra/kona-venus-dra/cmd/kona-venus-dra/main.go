// Command kona-venus-dra is the DRA kubelet plugin for krunkit GPU nodes.
// It runs as a DaemonSet on nodes labelled kona.dev/gpu=venus.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
	"k8s.io/klog/v2"

	"github.com/lestex/kona/dra/kona-venus-dra/pkg/driver"
)

func main() {
	klog.InitFlags(nil)
	nodeName := flag.String("node-name", os.Getenv("NODE_NAME"), "node this plugin runs on")
	cdiDir := flag.String("cdi-dir", "/var/run/cdi", "where containerd reads CDI specs")
	kubeletDir := flag.String("kubelet-dir", "/var/lib/kubelet", "kubelet root directory")
	flag.Parse()
	if err := run(*nodeName, *cdiDir, *kubeletDir); err != nil {
		klog.ErrorS(err, "kona-venus-dra failed")
		os.Exit(1)
	}
}

func run(nodeName, cdiDir, kubeletDir string) error {
	if nodeName == "" {
		return errors.New("--node-name or NODE_NAME is required")
	}
	out, err := exec.Command("vulkaninfo", "--summary").CombinedOutput()
	if err != nil {
		return fmt.Errorf("vulkaninfo: %w: %s", err, out)
	}
	gpu, err := driver.ParseVulkanSummary(string(out))
	if err != nil {
		return fmt.Errorf("%w (is %s present and the patched Mesa installed?)", err, driver.RenderNode)
	}
	if out, err = exec.Command("vulkaninfo").Output(); err != nil {
		return fmt.Errorf("vulkaninfo: %w", err)
	}
	if gpu.HeapBytes, err = driver.ParseVenusHeapBytes(string(out)); err != nil {
		return err
	}
	klog.InfoS("found Venus GPU", "device", gpu.DeviceName, "vulkanApiVersion", gpu.APIVersion, "heapBytes", gpu.HeapBytes)

	cfg, err := rest.InClusterConfig()
	if err != nil {
		return err
	}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	// The helper does not create its socket directories.
	pluginDir := filepath.Join(kubeletDir, "plugins", driver.DriverName)
	for _, dir := range []string{pluginDir, filepath.Join(kubeletDir, "plugins_registry"), cdiDir} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return err
		}
	}

	d := &driver.Driver{GPU: gpu, CDI: driver.CDI{Dir: cdiDir}, Cancel: cancel}
	helper, err := kubeletplugin.Start(ctx, d,
		kubeletplugin.DriverName(driver.DriverName),
		kubeletplugin.NodeName(nodeName),
		kubeletplugin.KubeClient(client),
		kubeletplugin.RegistrarDirectoryPath(filepath.Join(kubeletDir, "plugins_registry")),
		kubeletplugin.PluginDataDirectoryPath(pluginDir),
		kubeletplugin.HealthService(false),
	)
	if err != nil {
		return err
	}
	defer helper.Stop()
	if err := helper.PublishResources(ctx, d.Resources(nodeName)); err != nil {
		return err
	}
	klog.InfoS("published ResourceSlice", "driver", driver.DriverName, "node", nodeName)
	<-ctx.Done()
	if cause := context.Cause(ctx); cause != nil && !errors.Is(cause, context.Canceled) {
		return cause
	}
	return nil
}
