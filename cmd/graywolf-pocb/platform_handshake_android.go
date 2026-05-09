//go:build android

package main

import (
	"context"
	"log"
	"time"

	"github.com/chrissnell/graywolf/pkg/platformsvc"
)

func dialPlatformAndHello(sock string) error {
	c := platformsvc.NewClient(sock)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.ConnectWithReconnect(ctx); err != nil {
		return err
	}
	helloCtx, helloCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer helloCancel()
	resp, err := c.Hello(helloCtx, 1)
	if err != nil {
		return err
	}
	log.Printf("platformsvc: connected, server=%s schema=%d",
		resp.GetServerVersion(), resp.GetSchemaVersion())
	return nil
}
