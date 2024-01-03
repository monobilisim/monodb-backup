package backup

import (
	"errors"
	"github.com/aws/aws-sdk-go/service/s3"
	"monodb-backup/config"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
)

func newS3Uploader(region, accessKey, secretKey string) (*s3manager.Uploader, error) {
	sess, err := session.NewSessionWithOptions(session.Options{
		Profile: "default",
		Config: aws.Config{
			Region:      aws.String(region),
			Credentials: credentials.NewStaticCredentials(accessKey, secretKey, ""),
		},
	})
	if err != nil {
		return nil, err
	}
	uploader := s3manager.NewUploader(sess)
	return uploader, nil
}

func uploadFileToS3(uploader *s3manager.Uploader, src string, bucketName string, dst string, rotation config.Rotation, db, path string) error {
	file, err := os.Open(src)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = uploader.Upload(&s3manager.UploadInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(dst),
		Body:   file,
	})
	if err != nil {
		return err
	}
	if rotation.Enabled {
		shouldRotate, name := rotate(db, rotation.Period)
		if path != "" {
			name = path + "/" + name
		}
		extension := strings.Split(dst, ".")
		name = name + "." + extension[len(extension)-1]
		if shouldRotate {
			_, err := uploader.S3.CopyObject(&s3.CopyObjectInput{
				Bucket:     aws.String(bucketName),
				CopySource: aws.String(bucketName + "/" + dst),
				Key:        aws.String(name),
			})
			if err != nil {
				return errors.New("Couldn't create copy for rotation. Error: " + err.Error())
			}
		}
	}
	return nil
}
