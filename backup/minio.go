package backup

import (
	"context"
	"crypto/tls"
	"errors"
	"monodb-backup/config"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

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

func rotate(db, rotation string) (bool, string) {
	t := time.Now()
	_, week := t.ISOWeek()
	date := rightNow{
		month: time.Now().Format("January"),
		day:   time.Now().Format("Monday"),
	}
	switch rotation {
	case "month":
		yesterday := t.AddDate(0, 0, -1)
		if yesterday.Month() != t.Month() {
			return true, db + "-" + date.month
		}
	case "week":
		if date.day == "Monday" {
			return true, db + "-week_" + strconv.Itoa(week)
		}
	}
	return false, ""
}

func newMinioClient(endpoint, accessKey, secretKey string, secure, insecureSkipVerify bool) (*MinioClient, error) {
	minioOptions := &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: secure,
	}
	if insecureSkipVerify {
		customTransport := http.DefaultTransport.(*http.Transport).Clone()
		customTransport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		minioOptions.Transport = customTransport
	}
	minioClient, err := minio.New(endpoint, minioOptions)
	if err != nil {
		return nil, err
	}

	c := MinioClient{
		Client:             minioClient,
		Endpoint:           endpoint,
		AccessKey:          accessKey,
		SecretKey:          secretKey,
		Secure:             secure,
		InsecureSkipVerify: insecureSkipVerify,
	}

	return &c, nil
}

func uploadFileToMinio(minioClient *MinioClient, rotation config.Rotation, db string, src string, bucketName string, dst string, minio_dst string) error {
	src = strings.TrimSuffix(src, "/")
	_, err := os.Open(src)
	if err != nil {
		return err
	}
	_, err = minioClient.FPutObject(context.Background(), bucketName, dst, src, minio.PutObjectOptions{})
	if err != nil {
		return err
	}
	if rotation.Enabled {
		shouldRotate, name := rotate(db, rotation.Period)
		if shouldRotate {
			source := minio.CopySrcOptions{
				Bucket: bucketName,
				Object: dst,
			}
			extension := strings.Split(dst, ".")
			name = name + "." + extension[len(extension)-1]
			dest := minio.CopyDestOptions{
				Bucket: bucketName,
				Object: minio_dst + "/" + name,
			}
			_, err := minioClient.CopyObject(context.Background(), dest, source)
			if err != nil {
				return errors.New("Couldn't create copy for rotation. Error: " + err.Error())
			}
		}
	}
	return nil
}
