# pgsql-backup

[![Contributors][contributors-shield]][contributors-url]
[![Forks][forks-shield]][forks-url]
[![Stargazers][stars-shield]][stars-url]
[![Issues][issues-shield]][issues-url]
[![GPL License][license-shield]][license-url]

[![Readme in English](https://img.shields.io/badge/Readme-English-blue)](README.md)
[![Readme in Turkish](https://img.shields.io/badge/Readme-Turkish-red)](README.tr.md)

**pgsql-backup** is a tool for backing up PostgreSQL databases.

---

## Table of Contents

- [pgsql-backup](#pgsql-backup)
  - [Table of Contents](#table-of-contents)
  - [Features](#features)
  - [Usage](#usage)
  - [Configuration](#configuration)
  - [Building](#building)
  - [License](#license)

---

## Features

- Backs up all databases or a custom list of databases.
- Supports local backups and cloud storage options like S3 or Minio.
- Option to remove old local backups for efficient storage management.
- Provides notifications through email or Mattermost for monitoring backups.

---

## Usage

1. Configure pgsql-backup by editing the config file (default is `/etc/pgsql-backup.yml` )

2. Run the backup using the following command as the postgres user:

```
pgsql-backup
```

Backups will be created for each database based on the configuration. For local backups, ensure that you define a backup folder with appropriate permissions.

---

## Configuration

The configuration file is in YAML format. The available options are:

- `backupDestination` - Local backup folder path
- `databases` - List of database names to back up, if empty all databases are backed up
- `removeLocal` - Remove old local backups if true
- `s3` - S3 configuration for backups
- `minio` - Minio configuration for backups
- `notify` - Email and Mattermost notification configuration
- `log` - Logging configuration

See `config/config.sample.yml` for an example configuration file.

---

## Building

To build pgsql-backup:

```
go build cmd/pgsql-backup.go
```

---

## License

pgsql-backup is GPL-3.0 licensed. See [LICENSE](LICENSE) file for details.

[contributors-shield]: https://img.shields.io/github/contributors/monobilisim/pgsql-backup.svg?style=for-the-badge
[contributors-url]: https://github.com/monobilisim/pgsql-backup/graphs/contributors
[forks-shield]: https://img.shields.io/github/forks/monobilisim/pgsql-backup.svg?style=for-the-badge
[forks-url]: https://github.com/monobilisim/pgsql-backup/network/members
[stars-shield]: https://img.shields.io/github/stars/monobilisim/pgsql-backup.svg?style=for-the-badge
[stars-url]: https://github.com/monobilisim/pgsql-backup/stargazers
[issues-shield]: https://img.shields.io/github/issues/monobilisim/pgsql-backup.svg?style=for-the-badge
[issues-url]: https://github.com/monobilisim/pgsql-backup/issues
[license-shield]: https://img.shields.io/github/license/monobilisim/pgsql-backup.svg?style=for-the-badge
[license-url]: https://github.com/monobilisim/pgsql-backup/blob/master/LICENSE.txt