package backup

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"
	"monodb-backup/config"
	"strconv"
	"time"
)

type rightNow struct {
	year       string
	month      string
	day        string
	hour       string
	minute     string
	hourOnly   string
	minuteOnly string
	now        string
}

var dateNow rightNow

func dumpName(db string, rotation config.Rotation, buName string) string {
	if !rotation.Enabled {
		date := rightNow{
			year:  time.Now().Format("2006"),
			month: time.Now().Format("01"),
			now:   time.Now().Format("2006-01-02-150405"),
		}
		var name string
		if !params.BackupAsTables || db == "mysql_users" {
			name = date.year + "/" + date.month + "/" + db + "-" + date.now
		} else {
			name = date.year + "/" + date.month + "/" + db + "/" + buName + "-" + date.now
		}
		return name
	} else {
		suffix := rotation.Suffix
		if !params.BackupAsTables {
			switch suffix {
			case "day":
				return db + "-" + dateNow.day
			case "hour":
				return db + "-" + dateNow.hour
			case "minute":
				return db + "-" + dateNow.minute
			case "custom":
				if dateNow.minuteOnly == "00" {
					return db + "-" + dateNow.hourOnly
				} else {
					return db + "-" + dateNow.minuteOnly
				}
			default:
				return db + "-" + dateNow.day
			}
		} else {
			if db == "mysql_users" {
				db = "mysql"
				buName = "mysql_users"
			}
			switch suffix { //TODO + db + "/" +
			case "day":
				return db + "/" + buName + "-" + dateNow.day
			case "hour":
				return db + "/" + buName + "-" + dateNow.hour
			case "minute":
				return db + "/" + buName + "-" + dateNow.minute
			default:
				return db + "/" + buName + "-" + dateNow.day
			}
		}
	}
}

func rotate(db string) (bool, string) {
	t := time.Now()
	_, week := t.ISOWeek()
	date := rightNow{
		month: time.Now().Format("Jan"),
		day:   time.Now().Format("Mon"),
	}
	switch config.Parameters.Rotation.Period {
	case "month":
		yesterday := t.AddDate(0, 0, -1)
		if yesterday.Month() != t.Month() {
			return true, "Monthly/" + db + "-" + date.month
		}
	case "week":
		if date.day == "Mon" {
			var dst string
			if week <= 3 {
				dst = "Weekly/" + db + "-week_" + strconv.Itoa(49+week)
			} else {
				dst = "Weekly/" + db + "-week_" + strconv.Itoa(week-3)
			}
			for _, sess := range Sessions {
				if sess.Instance.Path != "" {
					dst = sess.Instance.Path + "/" + dst
				}
				input := &s3.DeleteObjectInput{
					Bucket: aws.String(sess.Instance.Bucket),
					Key:    aws.String(dst),
				}
				_, err := sess.Client.DeleteObject(input)
				if err != nil {
					logger.Error("Error while deleting " + dst + "at " + sess.Instance.Endpoint + "for rotation" + " - " + err.Error())
				}
			}
			return true, "Weekly/" + db + "-week_" + strconv.Itoa(week)
		}
	}
	return false, ""
}

func nameWithPath(name string) (newName string) {
	if !params.Rotation.Enabled {
		newName = name
	} else if params.Rotation.Special {
		if dateNow.minuteOnly == "00" {
			newName = "Hourly/" + dateNow.hourOnly + "/" + name
		} else {
			newName = "Last20Minutes/" + name
		}
	} else {
		suffix := params.Rotation.Suffix
		switch suffix {
		case "day":
			newName = "Daily/" + dateNow.day + "/" + name
		case "hour":
			newName = "Hourly/" + dateNow.day + "/" + dateNow.hour + "/" + name
		case "minute":
			newName = "Custom/" + dateNow.day + "/" + dateNow.hour + "/" + name
		default:
			newName = "Daily/" + name
		}
	}
	return
}
