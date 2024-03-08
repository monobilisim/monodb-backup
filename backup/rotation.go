package backup

import (
	"monodb-backup/config"
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

func dumpName(db string, params config.Rotation) string {
	if !params.Enabled {
		date := rightNow{
			year:  time.Now().Format("2006"),
			month: time.Now().Format("01"),
			now:   time.Now().Format("2006-01-02-150405"),
		}
		name := date.year + "/" + date.month + "/" + db + "-" + date.now
		return name
	} else {
		suffix := params.Suffix
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
			return true, "Weekly/" + db + "-week_" + strconv.Itoa(week)
		}
	}
	return false, ""
}

func nameWithPath(db, name string) (newName string) {
	if !params.Rotation.Enabled {
		date := rightNow{
			year:  time.Now().Format("2006"),
			month: time.Now().Format("01"),
			now:   time.Now().Format("2006-01-02-150405"),
		}
		newName = date.year + "/" + date.month + "/" + db + "-" + date.now
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
