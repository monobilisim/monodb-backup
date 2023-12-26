[![Contributors][contributors-shield]][contributors-url]
[![Forks][forks-shield]][forks-url]
[![Stargazers][stars-shield]][stars-url]
[![Issues][issues-shield]][issues-url]
[![GPL License][license-shield]][license-url]

[![Readme in English](https://img.shields.io/badge/Readme-English-blue)](README.md)
[![Readme in Turkish](https://img.shields.io/badge/Readme-Turkish-red)](README.tr.md)

<div align="center"> 
<a href="https://mono.net.tr/">
  <img src="https://monobilisim.com.tr/images/mono-bilisim.svg" width="340"/>
</a>

<h2 align="center">monodb-backup</h2>
<b>monodb-backup</b>, PostgreSQL veritabanlarını yedeklemek için bir araçtır.
</div>

---

## İçindekiler 

- [İçindekiler](#i̇çindekiler)
- [Özellikler](#özellikler)
- [Kullanım](#kullanım)
- [Gereksinimler](#gereksinimler)
- [Yapılandırma](#yapılandırma)
- [Derleme](#derleme)
- [Lisans](#lisans)

---

## Özellikler

- Tüm veritabanlarını veya özel bir veritabanı listesini yedekler ve şifreler.
- Yerel yedeklemeleri ve S3 veya Minio gibi bulut depolama seçeneklerini destekler.
- Verimli depolama yönetimi için eski yerel yedekleri kaldırma seçeneği.
- Yedeklemeleri izlemek için e-posta aracılığıyla bildirimler sağlar.

---

## Kullanım

1. Yapılandırma dosyasını düzenleyerek monodb-backup'ı yapılandırın (Konum `/etc/monodb-backup.yml` )

2. Yedeklemeyi postgres kullanıcısı ile aşağıdaki komutla çalıştırın:

```
monodb-backup
```

Yapılandırmaya bağlı olarak her veritabanı için yedekler oluşturulacaktır. Yerel yedekler için bir yedekleme klasörü tanımlanmalıdır, ve klasör için gerekli yetkilerin verilmesi gerekmektedir. 

---

## Gereksinimler

- p7zip

---

## Yapılandırma

Yapılandırma dosyası YAML biçimindedir. Mevcut seçenekler şunlardır:

- `backupDestination` - Yerel yedekleme klasörü yolu
- `databases` - Yedeklenecek veritabanı adlarının listesi, eğer boş bırakılırsa tüm veritabanları yedeklenir.
- `removeLocal` - true ise eski yerel yedekleri kaldırır
- `archivePass` - Yedekleri 7z ile şifrelerken kullanılacak parola.
- `s3` - Yedeklemeler için S3 yapılandırması
- `minio` - Yedeklemeler için Minio yapılandırması
- `notify` - E-posta ve webhook bildirim yapılandırması
- `log` - log yapılandırması

Örnek bir yapılandırma dosyası için `config/config.sample.yml` dosyasına bakın.

---

## Derleme

monodb-backup derlemek için:

```
CGO_ENABLED=0 go build -ldflags '-extldflags "-static"'
```

---

## Lisans

monodb-backup GPL-3.0 lisanslıdır. Ayrıntılar için [LICENSE](LICENSE) dosyasına bakın.

[contributors-shield]: https://img.shields.io/github/contributors/monobilisim/monodb-backup.svg?style=for-the-badge
[contributors-url]: https://github.com/monobilisim/monodb-backup/graphs/contributors
[forks-shield]: https://img.shields.io/github/forks/monobilisim/monodb-backup.svg?style=for-the-badge
[forks-url]: https://github.com/monobilisim/monodb-backup/network/members
[stars-shield]: https://img.shields.io/github/stars/monobilisim/monodb-backup.svg?style=for-the-badge
[stars-url]: https://github.com/monobilisim/monodb-backup/stargazers
[issues-shield]: https://img.shields.io/github/issues/monobilisim/monodb-backup.svg?style=for-the-badge
[issues-url]: https://github.com/monobilisim/monodb-backup/issues
[license-shield]: https://img.shields.io/github/license/monobilisim/monodb-backup.svg?style=for-the-badge
[license-url]: https://github.com/monobilisim/monodb-backup/blob/master/LICENSE