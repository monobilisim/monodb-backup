[![Contributors][contributors-shield]][contributors-url]
[![Forks][forks-shield]][forks-url]
[![Stargazers][stars-shield]][stars-url]
[![Issues][issues-shield]][issues-url]
[![GPL License][license-shield]][license-url]

[![Readme in English](https://img.shields.io/badge/Readme-English-blue)](README.md)
[![Readme in Turkish](https://img.shields.io/badge/Readme-Turkish-red)](README.tr.md)

<div align="center"> 
<a href="https://monobilisim.com.tr/">
  <img src="https://monobilisim.com.tr/images/mono-bilisim.svg" width="340"/>
</a>

<h2 align="center">pgsql-backup</h2>
<b>pgsql-backup</b>, PostgreSQL veritabanlarını yedeklemek için bir araçtır.
</div>

---

## İçindekiler 

- [İçindekiler](#i̇çindekiler)
- [Özellikler](#özellikler)
- [Kullanım](#kullanım)
- [Yapılandırma](#yapılandırma)
- [Derleme](#derleme)
- [Lisans](#lisans)

---

## Özellikler

- Tüm veritabanlarını veya özel bir veritabanı listesini yedekler.
- Yerel yedeklemeleri ve S3 veya Minio gibi bulut depolama seçeneklerini destekler.
- Verimli depolama yönetimi için eski yerel yedekleri kaldırma seçeneği.
- Yedeklemeleri izlemek için e-posta veya Mattermost aracılığıyla bildirimler sağlar.

---

## Kullanım

1. Yapılandırma dosyasını düzenleyerek pgsql-backup'ı yapılandırın (Konum `/etc/pgsql-backup.yml` )

2. Yedeklemeyi postgres kullanıcısı ile aşağıdaki komutla çalıştırın:

```
pgsql-backup
```

Yapılandırmaya bağlı olarak her veritabanı için yedekler oluşturulacaktır. Yerel yedekler için bir yedekleme klasörü tanımlanmalıdır, ve klasör için gerekli yetkilerin verilmesi gerekmektedir. 

---

## Yapılandırma

Yapılandırma dosyası YAML biçimindedir. Mevcut seçenekler şunlardır:

- `backupDestination` - Yerel yedekleme klasörü yolu
- `databases` - Yedeklenecek veritabanı adlarının listesi, eğer boş bırakılırsa tüm veritabanları yedeklenir.
- `removeLocal` - true ise eski yerel yedekleri kaldırır
- `s3` - Yedeklemeler için S3 yapılandırması
- `minio` - Yedeklemeler için Minio yapılandırması
- `notify` - E-posta ve Mattermost bildirim yapılandırması
- `log` - log yapılandırması

Örnek bir yapılandırma dosyası için `config/config.sample.yml` dosyasına bakın.

---

## Derleme

pgsql-backup derlemek için:

```
CGO_ENABLED=0 go build -ldflags '-extldflags "-static"'
```

---

## Lisans

pgsql-backup GPL-3.0 lisanslıdır. Ayrıntılar için [LICENSE](LICENSE) dosyasına bakın.

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