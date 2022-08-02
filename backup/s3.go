package backup

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"os"
)

func newS3Uploader(region, accessKey, secretKey string) (*s3manager.Uploader, error)  {
	sess, err := session.NewSessionWithOptions(session.Options{
		Profile: "default",
		Config: aws.Config{
			Region: aws.String(region),
			Credentials: credentials.NewStaticCredentials(accessKey, secretKey, ""),
		},
	})
	if err != nil {
		return nil, err
	}
	uploader := s3manager.NewUploader(sess)
	return uploader, nil
}

func uploadFileToS3(uploader *s3manager.Uploader, filePath string, bucketName string, fileName string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = uploader.Upload(&s3manager.UploadInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(fileName),
		Body:   file,
	})
	return err
}
