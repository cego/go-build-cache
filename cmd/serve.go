package cmd

import (
	"log"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/cego/go-build-cache/internal/cache"
	"github.com/cego/go-build-cache/internal/server"
	"github.com/spf13/cobra"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run the HTTP cache server backed by local disk (L1) and S3 (L2)",
	RunE:  runServe,
}

var serveFlags struct {
	listen      string
	cacheDir    string
	bucket      string
	prefix      string
	endpointURL string
	region      string
	pathStyle   bool
	verbose     bool
}

func init() {
	rootCmd.AddCommand(serveCmd)
	f := serveCmd.Flags()
	f.StringVar(&serveFlags.listen, "listen", ":5930", "listen address")
	f.StringVar(&serveFlags.cacheDir, "cache-dir", "", "local disk L1 cache directory; empty means OS default")
	f.StringVar(&serveFlags.bucket, "bucket", "", "S3 bucket (required)")
	f.StringVar(&serveFlags.prefix, "prefix", "", "S3 key prefix")
	f.StringVar(&serveFlags.endpointURL, "s3-endpoint-url", "", "S3 endpoint URL")
	f.StringVar(&serveFlags.region, "region", "us-east-1", "S3 region")
	f.BoolVar(&serveFlags.pathStyle, "s3-path-style", false, "use S3 path-style addressing")
	f.BoolVar(&serveFlags.verbose, "verbose", false, "be verbose")
}

func runServe(cmd *cobra.Command, _ []string) error {
	if serveFlags.bucket == "" {
		return cmd.Usage()
	}

	dir, err := resolveCacheDir(serveFlags.cacheDir)
	if err != nil {
		return err
	}

	ctx := cmd.Context()
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(serveFlags.region))
	if err != nil {
		return err
	}
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		if serveFlags.endpointURL != "" {
			o.BaseEndpoint = aws.String(serveFlags.endpointURL)
		}
		o.UsePathStyle = serveFlags.pathStyle
	})

	disk := &cache.DiskCache{Dir: dir}
	c := cache.NewS3Cache(disk, client, serveFlags.bucket, serveFlags.prefix, serveFlags.verbose)
	srv := &server.Server{Cache: c, Verbose: serveFlags.verbose}

	log.Printf("go-build-cache serve: listening on %s, bucket=%s, dir=%s", serveFlags.listen, serveFlags.bucket, dir)
	return http.ListenAndServe(serveFlags.listen, srv)
}
