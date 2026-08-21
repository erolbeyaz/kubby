# Kubby

Tarayıcı üzerinden çalışan, çok-cluster **Kubernetes yönetim ve gözlem arayüzü** —
Lens/Rancher muadili, kendi altyapında çalışan bir internal tool.

Çözdüğü problem: birden fazla cluster'ı (prod / preprod / test / fkm) yönetirken masaüstü
Lens kurulumuna, dağınık kubeconfig dosyalarına ve sürekli terminal açmaya bağımlı kalmak.
Kubby bunu tek bir merkezi web arayüzüne taşır.

> **Durum: tasarım aşaması.** Henüz kod yazılmadı. Faz planı `docs/ROADMAP.md`'de ve
> kullanıcı onayı bekliyor. Aşağıdaki kurulum bölümü Faz 1'de doldurulacaktır.

---

## Ne yapar

- **Sağlık paneli** — açılışta cluster'daki bozuk olan her şey tek ekranda:
  CrashLoopBackOff, ImagePullBackOff, OOMKilled, Pending pod'lar, NotReady node'lar,
  Failed job'lar, Bound olmayan PVC'ler, Warning event'ler, süresi yaklaşan sertifikalar
- **Sorun tespiti** — pod satırında tek tıkla log ve describe; restart sebebi (exitCode,
  OOMKilled, Error) pod'un içine girmeden listede rozet olarak görünür
- **Tam kaynak gözlemi** — Workload, Config, Network (Gateway API dahil), Storage,
  Access Control, CRD'ler için jenerik görüntüleyici
- **Obje işlemleri** — YAML düzenleme (server-side dry-run + diff), scale, rollout
  restart/rollback, delete, node cordon/drain
- **Terminal** — pod exec, kubectl web terminali, port-forward
- **Denetim** — her yıkıcı işlem audit log'a: kim, ne zaman, hangi cluster, ne yaptı

## Güvenlik

Kubby cluster'lara yüksek yetkiyle erişir; güvenlik tasarımın temel şartıdır:
kubeconfig'ler AES-256-GCM ile at-rest şifreli, argon2id + TOTP 2FA, sunucu tarafında
zorunlu yetki kontrolü, opsiyonel Kubernetes RBAC impersonation, kapatılamaz audit akışı,
distroless non-root imaj. Ayrıntı: [`docs/SECURITY.md`](docs/SECURITY.md).

## Teknoloji Yığını

| Katman | Seçim |
|---|---|
| Backend | Go 1.27.0 + client-go v0.36.4 + chi |
| Frontend | React 19.2.8 + TypeScript 7.0.2 + Vite 8.2.2 + Tailwind 4.3.3 |
| Veritabanı | PostgreSQL 18.6 (pgx v5 + goose) |
| Terminal / Editör | xterm.js 6.0.0 · Monaco 0.56.0 |
| Dağıtım | Tek distroless imaj · Helm chart |

Gerekçeler: [`docs/DECISIONS.md`](docs/DECISIONS.md).

## Kurulum

`TODO:` Faz 1'de doldurulacak.

**Ön koşullar:** Docker + Docker Compose, Go 1.27, Node 24.19 LTS.

```bash
cp .env.example .env      # doldur — .env asla commit edilmez
make gen-key              # KUBBY_ENCRYPTION_KEY uretir, ciktiyi .env'e yaz
git config core.hooksPath .githooks   # gitleaks pre-commit hook'unu etkinlestir
make setup
make dev                  # http://localhost:8080
```

İlk açılışta kurulum sihirbazı ilk admin hesabını oluşturur. Başka kayıt yolu yoktur.

> `KUBBY_ENCRYPTION_KEY` boş, kısa veya örnek değerdeyse uygulama **açılmaz**.
> Bu anahtar kaybolursa kayıtlı kubeconfig'ler geri getirilemez.

### Kendi registry'n ile build

Tüm base image referansları `ARG REGISTRY` ile parametrelidir (ADR-027):

```bash
# Kendi registry'n uzerinden build
docker build \
  --build-arg REGISTRY=my-registry.local \
  -t my-registry.local/kubby:0.1.0 .

docker push my-registry.local/kubby:0.1.0

# compose de ayni degiskeni kullanir
REGISTRY=my-registry.local docker compose up -d
```

Helm ile kurarken:

```bash
helm install kubby deploy/helm/kubby \
  --set image.registry=my-registry.local \
  --set image.repository=platform/kubby \
  --set image.tag=0.1.0
```

### Kurumsal Root CA

İç Elasticsearch ve API uçları özel bir Root CA ile imzalı. CA bundle'ı
mount edip `KUBBY_EXTRA_CA_BUNDLE` ile göster — sistem trust store'una **eklenir**,
onu değiştirmez (ADR-020).

## Geliştirme kuralları

- **Bağımlılık sürümleri sabittir** (ADR-025). `npm ci` kullanılır, `npm install` değil.
  Renovate/Dependabot **yoktur**; yükseltmeler bilinçli ve sürüm notu okunarak yapılır.
- **Sunucu UTC, arayüz Europe/Istanbul** (ADR-026). Testler `TZ=UTC` ile koşar.
- **Secret taraması:** `git config core.hooksPath .githooks` ile pre-commit hook'u
  etkinleştir. Aynı tarama CI'da da çalışır. `--no-verify` ile atlama.
- Faz başına feature branch (`phase-1-skeleton`); merge kararı proje sahibinde.
- Commit formatı ve branch stratejisi: [`docs/CONVENTIONS.md`](docs/CONVENTIONS.md).

## Dokümantasyon

| Dosya | İçerik |
|---|---|
| [`CLAUDE.md`](CLAUDE.md) | Proje kimliği, standartlar, çalışma yöntemi |
| [`docs/STATE.md`](docs/STATE.md) | Canlı durum: nerede kaldık, açık sorular |
| [`docs/ROADMAP.md`](docs/ROADMAP.md) | 10 fazlık plan ve bitti-sayılma kriterleri |
| [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) | Diyagram, veri modeli, API, ortam değişkenleri |
| [`docs/DECISIONS.md`](docs/DECISIONS.md) | ADR günlüğü — kararlar ve gerekçeleri |
| [`docs/CONVENTIONS.md`](docs/CONVENTIONS.md) | Kod stili, test, commit, branch |
| [`docs/SECURITY.md`](docs/SECURITY.md) | Tehdit modeli, önlemler, kalan riskler |

## Lisans

`TODO:` belirlenecek.
