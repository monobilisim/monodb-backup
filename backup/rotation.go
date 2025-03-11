package backup

import (
	"monodb-backup/config"
	"os"
	"strconv"
	"time"
)

type rightNow struct {
	year   string
	month  string
	day    string
	hour   string
	minute string
	now    string
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

func updateRotatedTimestamp(db string) {
	timestamp := time.Now().Format(time.RFC3339)
	err := os.WriteFile("/tmp/monodb-rotated-"+db, []byte(timestamp), 0644)
	if err != nil {
		logger.Error("Failed to update rotated timestamp: " + err.Error())
	}
}

func isRotated(db string) bool {
	timestamp, err := os.ReadFile("/tmp/monodb-rotated-" + db)
	if err != nil {
		logger.Error("Failed to read rotated timestamp: " + err.Error())
		return false
	}
	timestampTime, err := time.Parse(time.RFC3339, string(timestamp))
	if err != nil {
		logger.Error("Failed to parse rotated timestamp: " + err.Error())
		return false
	}
	return timestampTime.Add(23 * time.Hour).After(time.Now())
}

func rotate(db string) (bool, string) {
	if isRotated(db) {
		return false, ""
	}
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
			return true, "Weekly/" + db + "-week_" + strconv.Itoa(week)
		}
	}
	return false, ""
}

func nameWithPath(name string) (newName string) {
	if !params.Rotation.Enabled {
		newName = name
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
