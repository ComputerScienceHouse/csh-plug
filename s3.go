package main

import (
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/minio/minio-go"
	log "github.com/sirupsen/logrus"
)

type S3Connection struct {
	con *minio.Client
}

func (c *S3Connection) Init(host, access, secret string) {
	s3, err := minio.NewV2(host, access, secret, true)
	if err != nil {
		log.Fatal(err)
	}
	c.con = s3
}

func (c S3Connection) PresignPlug(plug Plug) *url.URL {
	presignedURL, err := c.con.PresignedGetObject("plugs", plug.S3ID, time.Duration(60)*time.Second, make(url.Values))
	if err != nil {
		log.Fatal(err)
	}

	return presignedURL
}

func (c S3Connection) AddFile(plug Plug, data io.Reader, mime string) {
	opts := new(minio.PutObjectOptions)
	opts.ContentType = mime
	_, err := c.con.PutObject("plugs", plug.S3ID, data, -1, *opts)
	if err != nil {
		log.Error(err)
	}
}

func (c S3Connection) DelFile(plug *Plug) error {
	err := c.con.RemoveObject("plugs", plug.S3ID)
	if err != nil {
		return fmt.Errorf("failed to remove plug object %s: %w", plug.S3ID)
	}

	return nil
}
