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

	"github.com/aws/aws-sdk-go/service/s3"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
)

type uploaderStruct struct {
	instance config.BackupTypeInfo
	uploader *s3manager.Uploader
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
	var options session.Options

	for _, s3Instance := range params.BackupType.Info {
		if s3Instance.Endpoint == "" {
			options = session.Options{
				Profile: "default",
				Config: aws.Config{
					Region:      aws.String(s3Instance.Region),
					Credentials: credentials.NewStaticCredentials(s3Instance.AccessKey, s3Instance.SecretKey, ""),
				},
			}

		} else {
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
				tr.TLSClientConfig.InsecureSkipVerify = true
			}
			httpClient := &http.Client{Transport: tr}

			options = session.Options{
				Profile:         "default",
				EC2IMDSEndpoint: s3Instance.Endpoint,
				Config: aws.Config{
					Endpoint:         &s3Instance.Endpoint,
					Region:           aws.String(s3Instance.Region),
					Credentials:      credentials.NewStaticCredentials(s3Instance.AccessKey, s3Instance.SecretKey, ""),
					S3ForcePathStyle: aws.Bool(true),
					HTTPClient:       httpClient,
				},
			}
		}
		sess, err := session.NewSessionWithOptions(options)
		if err != nil {
			notify.SendAlarm("Couldn't initialize S3 session. Error: "+err.Error(), true)
			logger.Fatal(err)
			return
		}
		uploaders = append(uploaders, uploaderStruct{s3Instance, s3manager.NewUploader(sess)})
	}
}

func uploadFileToS3(ctx context.Context, src, dst, db string, reader io.Reader, s3Instance *uploaderStruct) error {
	bucketName := s3Instance.instance.Bucket
	if reader == nil {
		src = strings.TrimSuffix(src, "/")
		file, err := os.Open(src)
		if err != nil {
			logger.Error("Couldn't open file " + src + " to read - Error: " + err.Error())
			notify.SendAlarm("Couldn't open file "+src+" to read - Error: "+err.Error(), true)
			return err
		}
		defer file.Close()
		logger.Info("Successfully opened file " + src + " to read.")
		reader = file
	} else {
		src = db
	}

	_, err := s3Instance.uploader.UploadWithContext(ctx, &s3manager.UploadInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(dst),
		Body:   reader,
	})
	if err != nil {
		logger.Error("Couldn't upload " + src + " to S3\nBucket: " + bucketName + " path: " + dst + "\n Error: " + err.Error())
		notify.SendAlarm("Couldn't upload "+src+" to S3\nBucket: "+bucketName+" path: "+dst+"\n Error: "+err.Error(), true)
		return err
	}
	logger.Info("Successfully uploaded " + src + " to S3\nBucket: " + bucketName + " path: " + dst)
	message := "Successfully uploaded " + src + " to S3\nBucket: " + bucketName + " path: " + dst
	notify.SendAlarm(message, false)
	itWorksNow(message, true)
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
			_, err := s3Instance.uploader.S3.CopyObject(&s3.CopyObjectInput{
				Bucket:     aws.String(bucketName),
				CopySource: aws.String(bucketName + "/" + dst),
				Key:        aws.String(name),
			})
			if err != nil {
				logger.Error("Couldn't create copy of " + src + " for rotation\nBucket: " + bucketName + " path: " + name + "\n Error: " + err.Error())
				notify.SendAlarm("Couldn't create copy of "+src+" for rotation\nBucket: "+bucketName+" path: "+name+"\n Error: "+err.Error(), true)
				return err
			}
			logger.Info("Successfully created a copy of " + src + " for rotation\nBucket: " + bucketName + " path: " + name)
			notify.SendAlarm("Successfully created a copy of "+src+" for rotation\nBucket: "+bucketName+" path: "+name, false)
		}
	}
	return nil
}

func uploadToS3(src, dst, db string) error {
	ctx := context.Background()
	for _, s3Instance := range uploaders {
		dst := nameWithPath(dst)
		if s3Instance.instance.Path != "" {
			dst = s3Instance.instance.Path + "/" + dst
		}
		err := uploadFileToS3(ctx, src, dst, db, nil, &s3Instance)
		if err != nil {
			return err
		}
	}
	return nil
}
