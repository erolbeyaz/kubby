# Değişiklikler

Sürümler [Semantic Versioning](https://semver.org/lang/tr/) izler. `0.x` boyunca minor
sürümler kırıcı değişiklik içerebilir.

---

## 0.13.0 — 2026-08-30

Seçili workload'ları topluca ölçekleme — DR tatbikatı için.

### Toplu ölçekleme

- Deployment, StatefulSet ve ReplicaSet listelerinde `−` ve `+` düğmelerinin sağında
  **SCALE** düğmesi. Seçilenlerin hepsi tek diyalogdan bir sayıya çekiliyor (ADR-152)
- **Restore previous**: Kubby ölçeklemeden önce mevcut sayıyı nesnenin üzerine
  `kubby.io/scaled-from` olarak yazıyor, geri alma her workload'ı **kendi** eski sayısına
  döndürüyor. Kubernetes böyle bir kayıt tutmadığı için, sıfıra çekilen yirmi
  deployment'ın yirmi farklı sayıya dönmesinin başka yolu yok
- İkinci kez sıfıra çekmek kaydı bozmuyor — aksi hâlde tatbikat, geri dönüşte hiçbir şey
  ayakta olmayan bir cluster ile biterdi
- Diyalog her workload'ın **şu an kaç replika koştuğunu** satır satır gösteriyor, sayılar
  birbirinden farklıysa uyarıyor; sıfır hedefi kırmızı
- Ölçekleme sırayla yapılıyor ve reddedilen her workload **adıyla** raporlanıyor
  (ArgoCD sahipliği, RBAC): kısmen biten bir toplu işlem sessizce başarılı sayılmıyor

---

## 0.12.0 — 2026-08-30

Port-forward artık gerçek bir port açıyor, node ve pod kendi detay ekranlarına kavuştu.

### Port-forward

- **Her forward Kubby'nin makinesinde gerçek bir TCP portu alıyor** (ADR-146). Uygulama
  kendi origin'ine kendi kökünde kavuşuyor; yol öneki altında mutlak asset yolları
  Kubby'ye çözülüyor ve tek sayfalık uygulamalar hiç açılmıyordu
- Varsayılan bind `127.0.0.1` — o port ham TCP, **kimlik doğrulama taşımıyor**
- Cluster içinde port tarayıcıya ulaşmadığı için **proxy'ye düşüyor** ve ekran hangisini
  kullandığını yazıyor. Helm: `config.forward.*` + `service.forwardPorts`
- **Port numarası düğme**: adrese tıkla → yeni sekme, yanındaki düğme → diyalog (yerel
  port, `https`, Open in Browser). Tünel açıkken düğme kırmızı `STOP` oluyor (ADR-147, 148)
- **Network → Port Forwarding** ekranı: ad, namespace, kind, pod portu, yerel port,
  protokol, yaş, durum; satırdan ve sağ tıkla Open/Stop
- Service de forward edilebiliyor; tünel arkasındaki hazır pod'a çözülüyor
- **Ingress ve HTTPRoute host'ları bağlantı** oldu; şema `spec.tls`'ten okunuyor.
  HTTPRoute'un hiç projeksiyonu yoktu — artık host, gateway ve kural sayısı var

### Node ve pod detay ekranları

- **Node** kendi paneline geçti (ADR-149): Metrics · Properties · Capacity · Allocatable ·
  Pods · Events. Koşullardan yalnızca doğru olanlar; kapasite okunur birimde
- **Pod** kendi paneline geçti (ADR-151): Metrics · Properties · Pod Volumes · Containers ·
  Events. Container başına durum, imaj, portlar, env, mount, probe, request/limit ve grafik
- **Pod başına metrik ucu** — filo yüküne eklenmiyor, panel açılınca iki sorgu
- Sekme satırında CPU/bellek geçişi; grafikte sağda değer ölçeği, altında 5 dakikalık
  zaman çizelgesi
- Her detay panelinin başlığında kopyalama düğmesi

### Düzeltmeler

- **Node kullanımı yanlış ölçülüyordu** (ADR-150). node-exporter makinenin belleğini
  raporluyor; k3d/minikube node'ları aynı `/proc`'u paylaştığı için **üçü de host'un
  rakamını** veriyordu — node'lar arasındaki fark tamamen siliniyordu. Artık kubelet'in
  cAdvisor'ından okunuyor ve `kubectl top node` ile tutuyor
- Birim yüzde değil çekirdek/bayt; ölçek 1/2/2.5/5 × 10ⁿ adımlarına oturuyor
- **Ray cluster'sız görünmüyordu** — taze kurulumda hiç gezinme yoktu, dolayısıyla ilk
  cluster'ı ekleme yolu ekranda çizilmiyordu
- Watch olayları listenin eklediği alanları taşımıyordu, log işaretleri bir saniyede
  siliniyordu (ADR-143)
- Node aksiyon sırası shell/cordon/drain/edit/delete; cordon artık `+` değil

---

## 0.11.0 — 2026-08-30

Bir pod `Running` ve `Ready` olabiliyor, sağlık probları geçiyor, ama logunda
veritabanına ulaşamadığını yazıyor. Kubernetes bunu bilmiyor; yüzlerce pod'da bu yalnızca
tek tek loglara girerek anlaşılıyor. Bu sürüm o soruyu ekrana taşıyor.

### Log kaynaklı sorun tespiti

- **Cluster başına Elasticsearch bağlantısı** (`/manage` → Logs), audit sink'inden
  bağımsız. Adres, index deseni ve kimlik bilgisi operatörden alınır; desen tahmin
  edilmez (ADR-138)
- **Bağlantı testi kaydetmeden çalışır** ve bir tam belge gösterir — hangi alanın mesajı,
  hangisinin pod adını taşıdığı oradan okunur
- **Dakikada tek sorgu, cluster başına.** Kubby log toplamaz; node'daki shipper zaten her
  satırı okuyor. Kubernetes API'sine ek istek gitmez. Üretim ortamında ölçülen: tek
  istek, 105 ms, 77 shard
- **Listede ayrı bir işaret** ve üzerine gelince kart: kural, sınıf, satır sayısı, ne
  kadar süredir devam ettiği, çıkarılmış özet (`database Orders · user svc-orders`) ve
  tek örnek satır. Kubernetes'in üçgeninden ayrı bir şekil — biri olgu, diğeri çıkarım
  (ADR-140)
- **Workload'a toplanıyor:** dokuz replikanın aynı hatası dokuz satır değil, bir satır
  ve "9 pods"
- **Sağlık panelinde `Application logs`** kategorisi; **Overview'da** en uzun süredir
  bozuk olan üstte
- **Kurallar, eşikler ve alan adları ayar** (Settings → Log analysis). Sessizce
  özelliği daraltacak her kayıt reddediliyor: ifadesiz kural, aynı addan iki tane,
  derlenmeyen yakalama deseni (ADR-144)
- **Ulaşılamayan kaynak `unknown`, asla "temiz"** (ADR-142)
- `make smoke-logfindings` — gerçek Elasticsearch'e karşı, `text` ve `keyword`
  eşlemesinin ikisinde de

### Liste ve detay ekranları

- **`Image` sütunu** Pod, Deployment, StatefulSet, DaemonSet, ReplicaSet, Job ve
  CronJob'da; detayda registry host'u, örtük `docker.io` yazılı, mirror çekimi
  yeniden yazdıysa `pulled` satırı (ADR-133)
- **Container kareleri artık durum taşıyor**, hazır sayısı değil: hangi container'ın
  bozuk olduğu pod'u açmadan görünüyor. Koyu kare "işini yaptı ve kapandı" demek.
  Karenin üzerinde exit code, sebep, başlangıç/bitiş, container ID (ADR-135)
- **Init container'lar kendi bölümünde**; `IMAGE / PORTS / REQUESTS / LIMITS / MOUNTS`
  ve pod düzeyinde `Volumes` — "Coming next" bölümünün yerinde, vaat ettiği üç faz da
  geldiği için (ADR-134)
- **Sütunlar sürüklenerek ölçekleniyor**, kind başına hatırlanıyor; çift tıklama
  varsayılana döndürüyor (ADR-137)

### Düzeltmeler

- **Geçmişteki restart artık işaret üretmiyor.** Bir kez restart olup o günden beri
  çalışan pod işaretliydi; sönmeyen bir işaret okuyucunun kaydırmayı öğrendiği bir
  işarettir (ADR-136)
- **Redaction `key=value` biçimindeki credential'ları kaçırıyordu.** Connection
  string'ler ve komut satırları parolayı böyle taşır; `Password=...;` artık maskeleniyor
- **Watch olayı satırı listede eklenen her şeyden yoksun bırakıyordu.** Yeni açılan bir
  watch tüm nesneleri `added` olarak tekrar gönderdiği için log işaretleri çizildikten
  bir saniye sonra siliniyordu (ADR-143)
- `SqlException` deseni PostgreSQL'in `PSQLException`'ını yakalayıp arızayı yanlış
  veritabanına yazıyordu (ADR-139)

### Şema

- `00007_cluster_logs.sql` — cluster başına log kaynağı; secret satıra bağlı mühürlü.
  Yükseltme yine bir etiket değişimi (ADR-105)

---

## 0.10.0 — 2026-08-26

Tek bir Overview, bir Home ekranı, yeniden tasarlanmış cluster yönetimi — ve Docker
Hub'da yayımlanan ilk imaj.

### Yayımlanan imaj

- **`docker.io/ebeyaz/kubby`** — imaj artık Docker Hub'da (ADR-131, ADR-110'un yerine).
  Kurmak için depoyu klonlamak ya da imaj derlemek gerekmiyor
- **`deploy/compose/docker-compose.published.yml`** — imajı adıyla taşıyan compose
  dosyası. Bir `.env` ve `docker compose up -d` ile kurulum tamam
- Etiket her zaman bir sürüm, asla `latest`: `latest` neyin çalıştığını söyleyemez ve
  geri alınamaz

### Gezinme

- **Tek Overview.** İki tasarım karşılaştırılıyordu; yenisi kazandı ve adı aldı (ADR-123)
- **Home** — ray'in başında sabit bir çıkış ve cluster listesi. Kartlar `Connected` /
  `Not connected` yazıyor, node/core/bellek/pod-slot taşıyor; **ulaşılamayan kart
  tıklanamıyor** (ADR-125, ADR-126, ADR-128)
- Ray'in üst bloğunda yalnızca **Nodes**; **Applications** Workloads'ın içinde
- Erişilemez hâle gelen eski Overview ve Workloads > Overview ekranları silindi (ADR-127)

### Cluster yönetimi

- **Kayıt ekranı ortam katmanlarına göre gruplanıyor**, production ilk. Bozuk olanlar
  sebebiyle en üstte adlandırılıyor; satırın tamamı cluster'ı açıyor (ADR-130)
- **Detay** yedi panellik kaydırma yerine durum taşıyan bir ray + bölümler; silme kendi
  bölümünde
- **Ekleme** iki numaralı adım; ortam dropdown yerine katmana tıklanarak seçiliyor
- Butonlar artık hover ve basılma durumu gösteriyor (ADR-131)

### Düzeltmeler

- **`topCpu` millicore döndürüyordu, panel "cores" yazıyordu** — `kube-system 61 cores`
  aslında 60m'di (ADR-120)
- **Aynı pod iki kez sayılıyordu:** imajı çekilemeyen pod hem "pending" hem
  "ImagePullBackOff"tu. Pending artık yalnızca yerleştirilmeyi bekleyen pod
- Tek bozuk pod problems listesinde üç kez görünüyordu (10→4 problem); son çıkış sebebi
  artık kazanan satırın altında
- Tek API server 2 sayılıyordu; restart sayısı ondalıklı yazılıyordu; committed yüzdeler
  capacity ile çarpılıyordu
- **Ağ grafiği** node başına iki çizgi yerine received/transmitted (ADR-124)
- **Trend grafiklerine zaman ekseni** eklendi (ADR-129)
- Placement paneli status paletinden ödünç aldığı renkleri bıraktı; doğrulanmış bir
  kimlik paleti kullanıyor (ADR-121)
- `fleetHealth` paylaşılan struct'a istek başına kapatma yazıyordu — çakışan iki istek
  birbirinin grant haritasını kullanabiliyordu (ADR-128)
- Status bar'daki monogram kaldırıldı

---

## 0.9.1 — 2026-08-26

Cluster Overview'ın tamamı ve verilen tasarıma göre ikinci bir overview ekranı.

### Cluster Overview

- Ekran **sol bardaki Overview'a** taşındı; Workloads > Overview iş yükü listelerine döndü
- **Türetilmiş cluster durumu** — skor değil, tetikleyen koşulun adıyla (ADR-114)
- **Prometheus otomatik bulunuyor** ve cluster'ın kendi API server'ı üzerinden okunuyor;
  elle adres girmek gerekmiyor (ADR-111)
- Node kartları/tablosu: kullanım **ve** taahhüt, pressure, swap, inode, disk I/O, ağ
  hataları, saat sapması, exporter erişilebilirliği
- Throttling, exit code, HPA, endpoint'siz servis, takılı rollout, geciken CronJob
- Sparkline, node condition görünümü, namespace × node heatmap
- **Her sayı tıklanabilir** ve ilgili nesneyi açıyor

### Overview 2

- Verilen tasarıma göre ikinci bir ekran
- Düzen tasarımın, renkler Kubby'nin
- **Lucide ikonları** projeye gömüldü — CDN yok, kaynak içinde (ISC, `NOTICE`)

### Bu sürümde düzeltilen kusurlar

- **`NaN` tüm cevabı düşürüyordu.** `histogram_quantile` boş histogramda `NaN` döner ve
  Go'nun JSON kodlayıcısı onu reddeder — tek bir quantile 178 KB'lık cevabın tamamını
  kodlanamaz hâle getiriyordu
- **`Point` JSON etiketsizdi** — `{At,Value}` gidiyor, istemci `{at,value}` bekliyordu;
  cevabın tamamı doğrulamadan düşüyor, panel sessizce sıfır çiziyordu (ADR-112)
- **Başarısız istek sağlıklı boş cluster gibi görünüyordu** — artık hata gösteriliyor
- **Cluster başına grant seviyesi** ve **exporter job regex'i** düzeltildi

---

## 0.9.0 — 2026-08-25

İlk paketlenmiş sürüm. Dokuz faz tamamlandı; Faz 10 (sertleştirme ve yayın) bu sürümle
kapandı. Üretimde kullanılmadan önce README'deki **Known limits** bölümü okunmalıdır.

### Kurulum

- **Tek imaj:** distroless, non-root (uid 65532), kabuksuz, salt-okunur kök. `kubectl`
  v1.36.4 ve `helm` v4.2.3 içinde — cluster terminali onları çalıştırır
- **Kubby şemasını kendisi kurar.** Ayrı bir migration adımı yok; yükseltme bir imaj
  etiketi değiştirmekten ibaret
- **Docker Compose** — `deploy/compose/`, iki container
- **Helm chart** — `deploy/helm/kubby/`; securityContext, NetworkPolicy, PDB, ve joker
  içermeyen en az yetkili ClusterRole
- `/readyz` şemayı da doğrular: ulaşılabilir bir veritabanı kullanılabilir demek değildir

### Cluster yönetimi

- Çok-cluster gezinme, her tür için sunucu tarafı projeksiyon
- Sağlık paneli: bozuk olan her şey tek ekranda, sebebiyle
- Yazma yolu: apply (dry-run + diff), scale, restart, rollback, delete, evict,
  cordon/drain, CronJob trigger/suspend — hepsi ArgoCD sahiplik farkındalığıyla
- Canlı güncelleme (SSE), iki yönlü sahiplik grafiği
- Pod kabuğu, node shell, ephemeral debug container, port-forward
- **Terminal** — `kubectl` ve `helm`'e kilitli, dosya sürüklenebilir
- Ctrl+K ile filo geneli arama; ulaşılamayan cluster **adıyla** raporlanır
- Helm release görünümü (değerler ve revizyon geçmişi)
- Prometheus entegrasyonu: cluster başına bağlantı, dashboard

### Güvenlik ve işletim

- argon2id · TOTP · kademeli kilitleme · CSRF · kapatılamaz audit
- Zarf şifreleme, satıra bağlı ciphertext; `make rotate-key`
- Audit gönderimi: Elasticsearch (data stream desteğiyle), Loki, HTTP/NDJSON
- `/metrics` — Kubby'nin kendi metrikleri, hiçbir zaman kimlik doğrulamasız
- `make config-export` / `config-restore` — parola korumalı şifreli arşiv
- CI: trivy, semgrep, gitleaks, npm audit, govulncheck — hepsi build'i düşürür

### Bu sürümde düzeltilen güvenlik kusurları

Faz 10'un güvenlik geçişinde bulundu:

- **Cluster başına grant seviyesi yazmayı engellemiyordu.** `read` grant'i yalnızca
  listelemede kullanılıyor, yazma yolunda hiç sorulmuyordu — prod bir cluster'da birine
  "read" vermek hiçbir şeyi durdurmuyordu
- **Port-forward proxy'si hata yolunda CSP'siz yanıt veriyordu**
- **Bozuk nesne adı 502 dönüyordu** — okuyucuyu çalışan bir cluster'ı incelemeye
  yönlendiriyordu; artık 400 ve sebebi
- **Ayar kaydetmek `/healthz`'i veritabanına bağımlı yapmıştı**; arka plan görevi panic'te
  tüm süreci düşürebiliyordu

### Bilinen sınırlar

- Tek replika hedeflenir (ADR-016)
- OIDC yok; kimlik yerel hesaplardır
- Loglar yalnızca Pod içindir
- Terminal cluster'ın kimlik bilgisini taşır
- `monaco-editor` bağımlılığında orta seviye bir XSS danışmanlığı

---

## Yayın nasıl yapılır

```bash
make test lint                                    # her şey yeşil olmalı
make release VERSION=0.12.0 IMAGE_REGISTRY=<hedef> # derle, doğrula, push
make tag VERSION=0.12.0                           # temiz ağaçta etiketle
git push origin v0.12.0
```

`make release` imajı push etmeden önce doğrular: doğru `kubectl`/`helm` sürümleri, uid
65532, kabuk yok. `make tag` kirli bir ağaçta çalışmaz — orada atılan bir etiket,
derlenen şeyi içermeyen bir commit'i gösterir.

`latest` etiketi **üretilmez**: hangi sürümün çalıştığını söyleyemez ve geri dönmeyi
imkânsız kılar.
