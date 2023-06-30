package main

import (
	"log"
	"os"
	"path"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/hairongchen/tdx-device-plugin/pkg/server"
)

func main() {

	log.Println("Intel TDX device plugin starting")
	tdxdpSrv := server.NewTdxDpServer()
	go tdxdpSrv.Run()

	if err := tdxdpSrv.RegisterToKubelet(); err != nil {
		log.Fatalf("register to kubelet error: %v", err)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatalf("Failed to created FS watcher.")
		os.Exit(1)
	}
	defer watcher.Close()

	err = watcher.Add(path.Dir(tdxdpSrv.KubeletSocket))
	if err != nil {
		log.Fatalf("watch kubelet error")
		return
	}
	for {
		select {
		case event := <-watcher.Events:
			if event.Name == tdxdpSrv.KubeletSocket && event.Op&fsnotify.Create == fsnotify.Create {
				time.Sleep(time.Second)
				log.Fatalf("restart TDX device plugin due to kubelet restart")
			}
			if event.Name == tdxdpSrv.tdxdpSocket && event.Op&fsnotify.Delete == fsnotify.Delete {
				log.Fatalf("restart TDX device plugin due to tdx device plugin socket being deleted")
			}
		case err := <-watcher.Errors:
			log.Fatalf("fsnotify watch error: %s", err)
		}
	}
}
