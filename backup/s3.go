package backup

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"monodb-backup/config"
	"monodb-backup/notify"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type uploaderStruct struct {
	instance config.BackupTypeInfo
	client   *s3.Client
	uploader *manager.Uploader
}

var uploaders []uploaderStruct

func mustGetSystemCertPool() *x509.CertPool {
	pool, err := x509.SystemCertPool()
	if err != nil {
		return x509.NewCertPool()
	}
	return pool
}

func InitializeS3Session() {
	ctx := context.Background()

	for _, s3Instance := range params.BackupType.Info {
		configOptions := []func(*awsconfig.LoadOptions) error{
			awsconfig.WithRegion(s3Instance.Region),
			awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
				s3Instance.AccessKey,
				s3Instance.SecretKey,
				"",
			)),
		}

		if s3Instance.Endpoint != "" {
			tr := &http.Transport{
				Proxy: http.ProxyFromEnvironment,
				DialContext: (&net.Dialer{
					Timeout:   30 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				MaxIdleConns:          256,
				MaxIdleConnsPerHost:   16,
				ResponseHeaderTimeout: time.Minute,
				IdleConnTimeout:       time.Minute,
				TLSHandshakeTimeout:   10 * time.Second,
				ExpectContinueTimeout: 10 * time.Second,
				DisableCompression:    true,
			}

			if s3Instance.Secure {
				tr.TLSClientConfig = &tls.Config{
					MinVersion: tls.VersionTLS12,
				}
				if f := os.Getenv("SSL_CERT_FILE"); f != "" {
					rootCAs := mustGetSystemCertPool()
					data, err := os.ReadFile(f)
					if err == nil {
						rootCAs.AppendCertsFromPEM(data)
					}
					tr.TLSClientConfig.RootCAs = rootCAs
				}
			}
			if s3Instance.InsecureSkipVerify {
				if tr.TLSClientConfig == nil {
					tr.TLSClientConfig = &tls.Config{}
				}
				tr.TLSClientConfig.InsecureSkipVerify = true
			}

			httpClient := &http.Client{Transport: tr}
			configOptions = append(configOptions, awsconfig.WithHTTPClient(httpClient))
		}

		cfg, err := awsconfig.LoadDefaultConfig(ctx, configOptions...)
		if err != nil {
			logger.Fatal("Couldn't initialize S3 config: " + err.Error())
			return
		}

		client := s3.NewFromConfig(cfg, func(o *s3.Options) {
			o.UsePathStyle = true
			if s3Instance.Endpoint != "" {
				o.BaseEndpoint = aws.String(s3Instance.Endpoint)
			}
		})

		uploader := manager.NewUploader(client, func(u *manager.Uploader) {
			u.PartSize = 64 * 1024 * 1024
			u.Concurrency = 10
		})

		uploaders = append(uploaders, uploaderStruct{
			instance: s3Instance,
			client:   client,
			uploader: uploader,
		})
	}
}

func uploadFileToS3(ctx context.Context, src, dst, db string, reader io.Reader, s3Instance *uploaderStruct) error {
	bucketName := s3Instance.instance.Bucket
	if reader == nil {
		src = strings.TrimSuffix(src, "/")
		file, err := os.Open(src)
		if err != nil {
			logger.Error("Couldn't open file " + src + " to read - Error: " + err.Error())
			return err
		}
		defer file.Close()
		logger.Info("Successfully opened file " + src + " to read.")
		reader = file
	} else {
		src = db
	}

	_, err := s3Instance.uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(dst),
		Body:   reader,
	})
	if err != nil {
		logger.Error("Couldn't upload " + src + " to S3\nBucket: " + bucketName + " path: " + dst + "\n Error: " + err.Error())
		return err
	}

	message := "Successfully uploaded " + src + " to S3\nBucket: " + bucketName + " path: " + dst
	logger.Info(message)

	if params.Rotation.Enabled {
		if db == "mysql" {
			db = db + "_users"
		}
		shouldRotate, name := rotate(db)
		if s3Instance.instance.Path != "" {
			name = s3Instance.instance.Path + "/" + name
		}
		extension := strings.Split(dst, ".")
		for i := 1; i < len(extension); i++ {
			name = name + "." + extension[i]
		}
		if shouldRotate {
			sourceObj, err := s3Instance.client.GetObject(ctx, &s3.GetObjectInput{
				Bucket: aws.String(bucketName),
				Key:    aws.String(dst),
			})
			if err != nil {
				logger.Error("Couldn't get source object for rotation\nBucket: " + bucketName + " path: " + dst + "\n Error: " + err.Error())
				return err
			}
			defer sourceObj.Body.Close()

			_, err = s3Instance.uploader.Upload(ctx, &s3.PutObjectInput{
				Bucket: aws.String(bucketName),
				Key:    aws.String(name),
				Body:   sourceObj.Body,
			})
			if err != nil {
				logger.Error("Couldn't create copy of " + src + " for rotation\nBucket: " + bucketName + " path: " + name + "\n Error: " + err.Error())
				return err
			}
			updateRotatedTimestamp(db)
			logger.Info("Successfully created a copy of " + src + " for rotation\nBucket: " + bucketName + " path: " + name)
		}
	}
	return nil
}

func uploadToS3(src, dst, db string) {
	ctx := context.Background()
	for _, s3Instance := range uploaders {
		finalDst := nameWithPath(dst)
		if s3Instance.instance.Path != "" {
			finalDst = s3Instance.instance.Path + "/" + finalDst
		}
		err := uploadFileToS3(ctx, src, finalDst, db, nil, &s3Instance)
		if err != nil {
			notify.FailedDBList = append(notify.FailedDBList, db+" - "+src+" - "+err.Error())
		} else {
			notify.SuccessfulDBList = append(notify.SuccessfulDBList, db+" - "+src)
		}
	}
}
