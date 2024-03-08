package backup

import (
	"context"
	"crypto/tls"
	"monodb-backup/notify"
	"net/http"
	"os"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type MinioClient struct {
	*minio.Client
	Endpoint           string
	AccessKey          string
	SecretKey          string
	Secure             bool
	InsecureSkipVerify bool
}

var mc *MinioClient

func InitializeMinioClient() {
	minioOptions := &minio.Options{
		Creds:  credentials.NewStaticV4(params.Minio.AccessKey, params.Minio.SecretKey, ""),
		Secure: params.Minio.Secure,
	}
	if params.Minio.InsecureSkipVerify {
		customTransport := http.DefaultTransport.(*http.Transport).Clone()
		customTransport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		minioOptions.Transport = customTransport
	}
	minioClient, err := minio.New(params.Minio.Endpoint, minioOptions)
	if err != nil {
		notify.SendAlarm("Couldn't initialize a MinIO client. Error: "+err.Error(), true)
		logger.Fatal(err)
		return
	}

	client := MinioClient{
		Client:             minioClient,
		Endpoint:           params.Minio.Endpoint,
		AccessKey:          params.Minio.AccessKey,
		SecretKey:          params.Minio.SecretKey,
		Secure:             params.Minio.Secure,
		InsecureSkipVerify: params.Minio.InsecureSkipVerify,
	}

	mc = &client
	return
}

func uploadFileToMinio(src, dst, db string) {
	src = strings.TrimSuffix(src, "/")
	bucketName := params.Minio.Bucket
	file, err := os.Open(src)
	if err != nil {
		logger.Error("Couldn't open file " + src + " to read - Error: " + err.Error())
		notify.SendAlarm("Couldn't open file "+src+" to read - Error: "+err.Error(), true)
		return
	}
	defer file.Close()

	_, err = mc.FPutObject(context.Background(), bucketName, dst, src, minio.PutObjectOptions{})
	if err != nil {
		logger.Error("Couldn't upload file " + src + " to MinIO\nBucket: " + bucketName + " path: " + dst + "\n Error: " + err.Error())
		notify.SendAlarm("Couldn't upload file "+src+" to MinIO\nBucket: "+bucketName+" path: "+dst+"\n Error: "+err.Error(), true)
		return
	}
	logger.Info("Successfully uploaded file " + src + " to MinIO\nBucket: " + bucketName + " path: " + dst)
	notify.SendAlarm("Successfully uploaded file "+src+" to MinIO\nBucket: "+bucketName+" path: "+dst, false)

	if params.Rotation.Enabled {
		shouldRotate, name := rotate(db)
		if shouldRotate {
			source := minio.CopySrcOptions{
				Bucket: bucketName,
				Object: dst,
			}
			extension := strings.Split(dst, ".")
			name = name + "." + extension[len(extension)-1]
			dest := minio.CopyDestOptions{
				Bucket: bucketName,
				Object: params.Minio.Path + "/" + name,
			}
			_, err := mc.CopyObject(context.Background(), dest, source)
			if err != nil {
				logger.Error("Couldn't create copy of " + src + " for rotation\nBucket: " + bucketName + " path: " + name + "\n Error: " + err.Error())
				notify.SendAlarm("Couldn't create copy of "+src+" for rotation\nBucket: "+bucketName+" path: "+name+"\n Error: "+err.Error(), true)
				return
			}
			logger.Info("Successfully created a copy of " + src + " for rotation\nBucket: " + bucketName + " path: " + name)
			notify.SendAlarm("Successfully created a copy of "+src+" for rotation\nBucket: "+bucketName+" path: "+name, false)
		}
	}
	return
}
