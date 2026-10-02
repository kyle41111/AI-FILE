package main

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/MythicMeta/MythicContainer/logging"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		cancel()
	}()

	var wg sync.WaitGroup
	for ctx.Err() == nil {
		InitializeLocalConfig()
		started := false
		for index, instance := range Config.Instances {
			if err := instance.applyDefaults(index + 1); err != nil {
				logging.LogError(err, "invalid listener configuration; waiting for config update", "instance", index+1)
				continue
			}
			started = true
			wg.Add(1)
			go func(instance instanceConfig) {
				defer wg.Done()
				if err := RunListener(ctx, instance); err != nil && ctx.Err() == nil {
					logging.LogError(err, "listener stopped", "instance", instance.Name)
				}
			}(instance)
		}
		if started {
			break
		}
		select {
		case <-ctx.Done():
		case <-time.After(15 * time.Second):
		}
	}
	wg.Wait()
}
