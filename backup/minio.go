package backup

import (
	"context"
	"crypto/tls"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"net/http"
	"os"
	"strings"
)

type MinioClient struct {
	*minio.Client
	Endpoint           string
	AccessKey          string
	SecretKey          string
	Secure             bool
	InsecureSkipVerify bool}

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

func uploadFileToMinio(minioClient *MinioClient, src string, bucketName string, dst string) error {
	src = strings.TrimSuffix(src, "/")
	_, err := os.Open(src)
	if err != nil {
		return err
	}
	_, err = minioClient.FPutObject(context.Background(), bucketName, dst, src, minio.PutObjectOptions{})
	if err != nil {
		return err
	}
	return nil
}
