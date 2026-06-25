package cache

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/bradfitz/go-tool-cache/cachers"
)

type actionMeta struct {
	OutputID string `json:"outputID"`
	Size     int64  `json:"size"`
}

// S3Cache is a two-tier cache: a local disk L1 in front of an S3 bucket L2.
type S3Cache struct {
	disk   *cachers.DiskCache
	client *s3.Client
	bucket string
	prefix string
	log    bool
}

func NewS3Cache(disk *cachers.DiskCache, client *s3.Client, bucket, prefix string, verbose bool) *S3Cache {
	return &S3Cache{disk: disk, client: client, bucket: bucket, prefix: prefix, log: verbose}
}

func (c *S3Cache) actionKey(actionID string) string { return c.prefix + "action/" + actionID }
func (c *S3Cache) outputKey(outputID string) string { return c.prefix + "output/" + outputID }

func (c *S3Cache) OutputFilename(outputID string) string { return c.disk.OutputFilename(outputID) }

func (c *S3Cache) Get(ctx context.Context, actionID string) (outputID, diskPath string, err error) {
	outputID, diskPath, err = c.disk.Get(ctx, actionID)
	if err == nil && outputID != "" {
		return outputID, diskPath, nil
	}

	meta, err := c.getMeta(ctx, actionID)
	if err != nil {
		return "", "", err
	}
	if meta == nil {
		return "", "", nil
	}

	obj, err := c.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(c.outputKey(meta.OutputID)),
	})
	if err != nil {
		if isNotFound(err) {
			return "", "", nil
		}
		return "", "", err
	}
	defer func() { _ = obj.Body.Close() }()

	diskPath, err = c.disk.Put(ctx, actionID, meta.OutputID, meta.Size, obj.Body)
	if err != nil {
		return "", "", err
	}
	return meta.OutputID, diskPath, nil
}

func (c *S3Cache) Put(ctx context.Context, actionID, outputID string, size int64, body io.Reader) (diskPath string, _ error) {
	buf, err := io.ReadAll(body)
	if err != nil {
		return "", err
	}

	diskPath, err = c.disk.Put(ctx, actionID, outputID, size, bytes.NewReader(buf))
	if err != nil {
		return "", err
	}

	if _, err := c.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(c.bucket),
		Key:           aws.String(c.outputKey(outputID)),
		Body:          bytes.NewReader(buf),
		ContentLength: aws.Int64(int64(len(buf))),
	}); err != nil {
		return "", err
	}

	metaBytes, err := json.Marshal(actionMeta{OutputID: outputID, Size: size})
	if err != nil {
		return "", err
	}
	if _, err := c.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(c.bucket),
		Key:           aws.String(c.actionKey(actionID)),
		Body:          bytes.NewReader(metaBytes),
		ContentLength: aws.Int64(int64(len(metaBytes))),
	}); err != nil {
		return "", err
	}

	return diskPath, nil
}

func (c *S3Cache) getMeta(ctx context.Context, actionID string) (*actionMeta, error) {
	obj, err := c.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(c.actionKey(actionID)),
	})
	if err != nil {
		if isNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = obj.Body.Close() }()

	var meta actionMeta
	if err := json.NewDecoder(obj.Body).Decode(&meta); err != nil {
		c.logf("ignoring corrupt action meta %s: %v", actionID, err)
		return nil, nil
	}
	return &meta, nil
}

func (c *S3Cache) logf(format string, args ...any) {
	if c.log {
		log.Printf(format, args...)
	}
}

func isNotFound(err error) bool {
	var nsk *types.NoSuchKey
	if errors.As(err, &nsk) {
		return true
	}
	var nf *types.NotFound
	return errors.As(err, &nf)
}
