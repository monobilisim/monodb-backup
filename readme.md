# pgsql-backup

pgsql-backup, PostgreSQL veritabanlarını yedeklemek için bir araçtır.

# Özellikler

- Tüm veritabanlarını veya seçilen veritabanları listesini yedekler
- Veritabanlarını yerel olarak ve/veya S3 veya Minio'ya döker
- Eski yerel yedekleri kaldırır
- E-posta veya Mattermost aracılığıyla bildirim gönderir

# Kullanım

Yapılandırma dosyasını düzenleyerek pgsql-backup'ı yapılandırın (Konum `/etc/pgsql-backup.yml` )

## Yedeklemeyi çalıştırın:

postgres kullanıcısı ile:

```
pgsql-backup
```

Yapılandırmaya bağlı olarak her veritabanı için yedekler oluşturulacaktır. Yerel yedekler için bir yedekleme klasörü tanımlanmalıdır, ve klasör için gerekli yetkilerin verilmesi gerekmektedir. 

# Yapılandırma

Yapılandırma dosyası YAML biçimindedir. Mevcut seçenekler şunlardır:

- `backupDestination` - Yerel yedekleme klasörü yolu
- `databases` - Yedeklenecek veritabanı adlarının listesi, eğer boş bırakılırsa tüm veritabanları yedeklenir.
- `removeLocal` - true ise eski yerel yedekleri kaldırır
- `s3` - Yedeklemeler için S3 yapılandırması
- `minio` - Yedeklemeler için Minio yapılandırması
- `notify` - E-posta ve Mattermost bildirim yapılandırması
- `log` - log yapılandırması

Örnek bir yapılandırma dosyası için `config/config.sample.yml` dosyasına bakın.

# Build

pgsql-backup oluşturmak için:

```
go build cmd/pgsql-backup.go
```

# Lisans

pgsql-backup GPL-3.0 lisanslıdır. Ayrıntılar için LICENSE dosyasına bakın.