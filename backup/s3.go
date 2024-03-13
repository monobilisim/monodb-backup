package backup

import (
	"monodb-backup/notify"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go/service/s3"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
)

var sess *session.Session
var uploader *s3manager.Uploader

func InitializeS3Session() {
	var err error
	endpoint := "play.min.io" //TODO delete later
	sess, err = session.NewSessionWithOptions(session.Options{
		Profile: "default",
		Config: aws.Config{
			Endpoint:         &endpoint, //TODO delete later
			Region:           aws.String(params.S3.Region),
			Credentials:      credentials.NewStaticCredentials(params.S3.AccessKey, params.S3.SecretKey, ""),
			S3ForcePathStyle: aws.Bool(true), //TODO delete later
		},
	})
	if err != nil {
		notify.SendAlarm("Couldn't initialize S3 session. Error: "+err.Error(), true)
		logger.Fatal(err)
		return
	}
	uploader = s3manager.NewUploader(sess)
}

func uploadFileToS3(src, dst, db string) {
	src = strings.TrimSuffix(src, "/")
	bucketName := params.S3.Bucket
	file, err := os.Open(src)
	if err != nil {
		logger.Error("Couldn't open file " + src + " to read - Error: " + err.Error())
		notify.SendAlarm("Couldn't open file "+src+" to read - Error: "+err.Error(), true)
		return
	}
	defer file.Close()
	logger.Info("Successfully opened file " + src + " to read.")
	_, err = uploader.Upload(&s3manager.UploadInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(dst),
		Body:   file,
	})
	if err != nil {
		logger.Error("Couldn't upload file " + src + " to S3\nBucket: " + bucketName + " path: " + dst + "\n Error: " + err.Error())
		notify.SendAlarm("Couldn't upload file "+src+" to S3\nBucket: "+bucketName+" path: "+dst+"\n Error: "+err.Error(), true)
		return
	}
	logger.Info("Successfully uploaded file " + src + " to S3\nBucket: " + bucketName + " path: " + dst)
	notify.SendAlarm("Successfully uploaded file "+src+" to S3\nBucket: "+bucketName+" path: "+dst, false)
	if params.Rotation.Enabled {
		shouldRotate, name := rotate(db)
		if params.S3.Path != "" {
			name = params.S3.Path + "/" + name
		}
		extension := strings.Split(dst, ".")
		for i := 1; i < len(extension); i++ {
			name = name + "." + extension[i]
		}
		if shouldRotate {
			_, err := uploader.S3.CopyObject(&s3.CopyObjectInput{
				Bucket:     aws.String(bucketName),
				CopySource: aws.String(bucketName + "/" + dst),
				Key:        aws.String(name),
			})
			if err != nil {
				logger.Error("Couldn't create copy of " + src + " for rotation\nBucket: " + bucketName + " path: " + name + "\n Error: " + err.Error())
				notify.SendAlarm("Couldn't create copy of "+src+" for rotation\nBucket: "+bucketName+" path: "+name+"\n Error: "+err.Error(), true)
				return
			}
			logger.Info("Successfully created a copy of " + src + " for rotation\nBucket: " + bucketName + " path: " + name)
			notify.SendAlarm("Successfully created a copy of "+src+" for rotation\nBucket: "+bucketName+" path: "+name, false)
		}
	}
}
