# Yerel test cluster'ları

Kubby'yi geliştirirken iki cluster kullanılır. İkisinin **farklı Kubernetes sürümünde**
olması bilinçlidir: desteklenen aralığın iki ucu birden test edilir.

| Cluster | Araç | Sürüm | Node | Ne için |
|---|---|---|---|---|
| `kubby-test` | k3d | v1.35.5 | 3 | Ana geliştirme cluster'ı, bozuk iş yükleri burada |
| `kubby-mini` | minikube | v1.34.4 | 3 | Hedef sürüm (ADR: v1.33/v1.34) + Prometheus |

---

## kubby-mini kurulumu

```bash
minikube start -p kubby-mini \
  --nodes=3 \
  --driver=docker \
  --container-runtime=containerd \
  --kubernetes-version=v1.34.4 \
  --cpus=2 --memory=2200mb \
  --embed-certs \
  --addons=metrics-server
```

**`--embed-certs` zorunludur.** Kubby dosya yoluna işaret eden kimlik bilgisi kabul etmez;
kubeconfig gömülü sertifika veya bearer token taşımalıdır (ADR-018).

### WSL2'de "Too many open files"

`Failed to create control group inotify object: Too many open files` hatası alınırsa
inotify instance kotası dolmuştur — WSL2 varsayılanı 128'dir ve birkaç node container'ı
bunu tüketir:

```bash
sudo tee /etc/sysctl.d/99-kubby-inotify.conf <<'EOF'
fs.inotify.max_user_instances = 1024
fs.inotify.max_user_watches = 1048576
EOF
sudo sysctl -p /etc/sysctl.d/99-kubby-inotify.conf
```

---

## Prometheus

```bash
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update prometheus-community

helm --kube-context kubby-mini upgrade --install prometheus \
  prometheus-community/prometheus --version 29.27.0 \
  -n monitoring --create-namespace \
  -f deploy/dev/prometheus-values.yaml
```

Erişim (WSL'den):

```bash
minikube ip -p kubby-mini            # 192.168.58.2
curl http://192.168.58.2:30090/-/ready
```

Kubby'ye `Kubby Settings → Metrics` altından bu adres girilir. **Basic auth bu test
kurulumunda yoktur**; ayar alanı gerçek dağıtımlar için vardır.

---

## Kubby'ye cluster ekleme

```bash
kubectl config view --raw --minify --flatten --context=kubby-mini
```

Çıktıdaki `server:` adresi `https://127.0.0.1:<port>` gelir — bu minikube'ün port
yönlendirmesidir. Node IP'siyle değiştir:

```
server: https://192.168.58.2:8443
```

Sonra çıktı Kubby'nin cluster ekleme ekranına yapıştırılır.

> Kubeconfig **repoya yazılmaz.** Geçici bir dosyaya çıkarılıp yapıştırıldıktan sonra
> silinir.
