# Kubby — Docker Compose ile kurulum

İki container: Kubby ve veritabanı. Başka hiçbir şey gerekmiyor.

Elasticsearch, Loki, Prometheus **burada yok ve olmayacak** — onlar Kubby'nin gönderdiği
sistemler, birlikte geldiği şeyler değil. Varsa Kubby'nin kendi ayar ekranından adresini
verirsin; yoksa her şey onlarsız çalışır.

---

## Kurulum — yayımlanan imajla

Depoyu klonlamak ya da imaj derlemek gerekmiyor. Boş bir dizinde:

```bash
curl -O https://raw.githubusercontent.com/erolbeyaz/kubby/main/deploy/compose/docker-compose.published.yml
curl -o .env https://raw.githubusercontent.com/erolbeyaz/kubby/main/deploy/compose/.env.published.example
```

`.env` içinde **iki değer zorunlu**:

| Değişken | Nasıl üretilir |
|---|---|
| `KUBBY_DB_PASSWORD` | `openssl rand -base64 24` |
| `KUBBY_ENCRYPTION_KEY` | `openssl rand -base64 32` |

> **Şifreleme anahtarını kaybedersen saklanan hiçbir kubeconfig açılamaz.** İlk
> başlatmadan önce yedeğini bu makinenin dışında bir yere al.

Sonra:

```bash
docker compose -f docker-compose.published.yml up -d
```

İmajın adı compose dosyasının içinde yazıyor (`docker.io/ebeyaz/kubby:<sürüm>`).
Yükseltmek bir etiket değişimi ve `up -d`; geri almak aynısının tersi. Etiket her zaman
bir sürüm, asla `latest` — `latest` neyin çalıştığını söyleyemez.

---

## Kurulum — kendi imajını dereliyorsan

```bash
cp .env.example .env
```

Bu yolda **üç değer zorunlu**: yukarıdaki ikisi ve `KUBBY_IMAGE` (kendi registry'ndeki
sabit sürüm etiketi). Sonra:

```bash
docker compose up -d
```

Kubby veritabanı şemasını **kendisi kurar**. Ayrı bir migration adımı yok.

Tarayıcı: `KUBBY_PUBLIC_URL` ne yazıyorsa oraya. İlk açılışta kurulum sihirbazı ilk
yöneticiyi oluşturmanı ister.

### Çalıştığını doğrula

```bash
curl -s http://localhost:8080/readyz
# {"status":"ok","checks":{"database":"ok","schema":"v6"}}
```

`schema` alanı önemli: veritabanına *ulaşılabildiğini* değil, *kullanılabilir* olduğunu
söyler.

---

## Cluster'lar aynı makinede container ise

Gerçek bir ağda hiçbir şey gerekmez — Kubby API server'a diğer her şey gibi yönlenir.

Ama cluster'ların **kendisi bu makinede container ise** (k3d, minikube, kind), her biri
kendi Docker bridge'inde durur ve bridge'ler birbirine kapalıdır. Kubby onları göremez,
ve belirti şudur: **kubeconfig'in kusursuz, cluster "unreachable" diyor.**

```bash
docker network ls          # cluster ağlarının adlarını gör
```

`docker-compose.local-clusters.yml` dosyasındaki adları kendi ağlarınla değiştir, sonra:

```bash
docker compose -f docker-compose.yml -f docker-compose.local-clusters.yml up -d
```

**Bir tuzak daha:** kubeconfig'deki adres. k3d'nin verdiği kubeconfig `127.0.0.1:6550`
gösterir — bu host'un portu, container içinden `127.0.0.1` container'ın kendisidir.
Cluster'ın kendi ağ adresini kullan:

```bash
k3d kubeconfig get <cluster> | sed 's|server: https://[^:]*:[0-9]*|server: https://172.20.0.2:6443|'
```

minikube zaten node IP'si verdiği için (`192.168.58.2:8443`) düzeltme gerektirmez.

Yerel bir cluster loopback adres taşıyorsa `.env`'de `KUBBY_ALLOW_LOOPBACK_CLUSTERS=true`
gerekir — Kubby bunu varsayılan olarak reddeder (SSRF).

---

## İmaj nereden geliyor

Compose hiçbir şey derlemez; imajı etiketiyle **çeker**. İmajın bir registry'de olması
gerekir — internete açık olması gerekmez, yereldeki bir registry yeter.

### Yerel registry

```bash
make registry-up          # 127.0.0.1:5000, veri kalıcı bir volume'da
make registry-list        # içinde ne var
```

### Sürüm yayınlama

```bash
make release VERSION=0.9.0 IMAGE_REGISTRY=localhost:5000
```

Bu tek komut: derler, **imajı doğrular** (`kubectl`/`helm` sürümleri, uid 65532, kabuk
yok), pushlar ve `.env`'e yazılacak satırı yazdırır.

> **İki ayrı registry var, karıştırma:**
> `REGISTRY` temel imajların (golang, node, distroless) **çekildiği** yer — kendi mirror'ın
> varsa burayı değiştirirsin. `IMAGE_REGISTRY` senin imajının **push edildiği** yer.
> İkisini aynı sanmak, yayınladığın registry'den `golang` imajı istemek demektir.

Docker Hub'a yayınlamak da aynı:

```bash
docker login
make release VERSION=0.9.0 IMAGE_REGISTRY=docker.io/<kullanıcı>
```

`latest` etiketi **bilerek üretilmiyor** — hangi sürümün çalıştığını söyleyemez ve geri
dönmeyi imkânsız kılar.

---

## Sürüm yükseltme

Bir imaj etiketi değiştirmekten ibaret. Yeni imaj kendi migration'larını taşır ve
başlarken uygular.

```bash
# 1. Ne çalışıyor, yazıp bir kenara koy — geri dönmen gerekirse bu lazım
curl -s http://localhost:8080/version

# 2. Veritabanını yedekle. Migration'lar ileri gider, geri değil.
docker compose exec -T postgres pg_dump -U kubby kubby | gzip > kubby-$(date +%F).sql.gz

# 3. Yeni sürümü yayınla, sonra .env'de etiketi yükselt
#    make release VERSION=0.9.1 IMAGE_REGISTRY=localhost:5000
#    KUBBY_IMAGE=localhost:5000/kubby:0.9.1

# 4. Yeni imajı çek ve değiştir
docker compose pull kubby
docker compose up -d kubby

# 5. Doğrula
curl -s http://localhost:8080/version   # yeni sürüm
curl -s http://localhost:8080/readyz    # şema numarası
docker compose logs kubby | grep -i schema
```

Log şunlardan birini der: yeni migration varsa `schema migrated from=6 to=7`, yoksa
`schema is up to date`. İkisi de normaldir; hiçbir şey dememesi değildir.

Veritabanı container'ına dokunulmaz; yalnızca Kubby değişir.

### Geri dönmek

```bash
# .env'de eski etikete dön, sonra
docker compose up -d kubby
```

**Ama dikkat:** yeni sürüm migration uyguladıysa şema ileri gitmiştir. Eski binary yeni
şemayla çalışmayabilir. Bu yüzden 2. adımdaki yedek var — gerçek geri dönüş şudur:

```bash
docker compose down
docker volume rm <proje>_kubby-db
docker compose up -d postgres
gunzip -c kubby-2026-08-25.sql.gz | docker compose exec -T postgres psql -U kubby kubby
docker compose up -d kubby      # .env'de eski etiketle
```

> Bu yüzden yükseltmeden önce yedek almak isteğe bağlı değil.

---

## Günlük işler

```bash
docker compose logs -f kubby           # loglar (JSON, secret'lar redakte)
docker compose restart kubby           # yeniden başlat
docker compose ps                      # durum
docker compose down                    # durdur (veri kalır)
docker compose down -v                 # HER ŞEYİ SİL, veritabanı dahil
```

Yedek:

```bash
docker compose exec -T postgres pg_dump -U kubby kubby | gzip > kubby-$(date +%F).sql.gz
```

---

## Ters vekil arkasındaysa

`.env` içinde:

```
KUBBY_PUBLIC_URL=https://kubby.sirket.com
KUBBY_TRUSTED_PROXIES=10.0.0.0/8
```

`KUBBY_PUBLIC_URL` doğru olmazsa **terminal ve log akışları çalışmaz** — WebSocket origin
kontrolü bu adrese bakar. `KUBBY_TRUSTED_PROXIES` boşsa `X-Forwarded-For` başlıklarına
güvenilmez; bu, istemcinin kendi IP'sini uydurmasına izin vermekten iyidir.

TLS'i Kubby'nin kendisi sonlandırsın istersen `KUBBY_TLS_CERT_FILE` ve
`KUBBY_TLS_KEY_FILE` verilir, dosyalar mount edilir.

---

## Container hakkında

Distroless, non-root (uid 65532), **kabuksuz**, salt-okunur kök dosya sistemi, tüm
capability'ler düşürülmüş. `/tmp` bellekte — terminal oturumunun geçici kubeconfig'i ve
oraya bırakılan dosyalar diske hiç yazılmaz ve oturumdan sonra kalmaz.

İçinde `kubectl` ve `helm` var; Kubby'nin cluster terminali onları çalıştırır.
