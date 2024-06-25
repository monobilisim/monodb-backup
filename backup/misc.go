package backup

import (
	"monodb-backup/clog"
	"monodb-backup/config"
)

var logger *clog.CustomLogger = &clog.Logger
var params *config.Params = &config.Parameters
