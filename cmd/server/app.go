package main

import (
	"log"
	"os"
	"path"

	"tdx-device-plugin/pkg/server"

	"github.com/fsnotify/fsnotify"
)

func main() {

	log.Println("Intel TDX device plugin starting")
	tdxdpsrv := server.NewTdxDpServer()
	go tdxdpsrv.Run()

	if err := tdxdpsrv.RegisterToKubelet(); err != nil {
		log.Fatalf("register to kubelet error: %v", err)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatalf("Failed to created FS watcher.")
		os.Exit(1)
	}
	defer watcher.Close()

	err = watcher.Add(path.Dir(server.KubeletSocket))
	if err != nil {
		log.Fatalf("watch kubelet error")
		return
	}
	for {
		select {
		case event := <-watcher.Events:
			if event.Name == server.KubeletSocket && event.Op&fsnotify.Create == fsnotify.Create {
				log.Fatalf("restart TDX device plugin due to kubelet restart")
			}
			if event.Name == server.TdxDpSocket && event.Op&fsnotify.Remove == fsnotify.Remove {
				log.Fatalf("restart TDX device plugin due to tdx device plugin socket being deleted")
			}
		case err := <-watcher.Errors:
			log.Fatalf("fsnotify watch error: %s", err)
		}
	}
}
