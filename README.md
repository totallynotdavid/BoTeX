# BoTeX

Un bot simple de WhatsApp que convierte fórmulas matemáticas en $\LaTeX$ a imágenes. Perfecto para compartir ecuaciones en un chat grupal de estudiantes.

## ⚙️ Instalación

Antes de comenzar, necesitarás Go, TeX Live e ImageMagick. Puedes instalarlos con los siguientes comandos:

1. Instalar Go (si no lo tienes)

```bash
curl -OL https://go.dev/dl/go1.23.5.linux-amd64.tar.gz
sudo tar -C /usr/local -xvf go1.23.5.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.profile
source ~/.profile
```

2. Instalar TeX Live (completo)

```bash
sudo apt-get install perl
cd /tmp
curl -L -o install-tl-unx.tar.gz https://mirror.ctan.org/systems/texlive/tlnet/install-tl-unx.tar.gz
tar -xzf install-tl-unx.tar.gz
cd install-tl-*
sudo perl ./install-tl --no-interaction
echo 'export PATH=$PATH:/usr/local/texlive/2024/bin/x86_64-linux' >> ~/.profile
source ~/.profile
```

3. Clonar y ejecutar el bot

```bash
git clone https://github.com/totallynotdavid/BoTeX
cd BoTeX
sudo apt-get install gcc build-essential
export CGO_ENABLED=1
go run .
```

## 📱 Uso

1. **Escanea el QR** que aparecerá en la terminal con WhatsApp cuando ejecutes <kbd>go run .</kbd>
2. Envía un mensaje con el formato:

   ```
   !latex <tu_ecuación>
   ```

3. Recibirás una imagen con tu ecuación renderizada

**Ejemplos:**

```
!latex x = \frac{-b \pm \sqrt{b^2 - 4ac}}{2a}
```

```
!latex \int_{a}^{b} f(x)\,dx = F(b) - F(a)
```

## 🚨 Solución de problemas

Si encuentras errores:

```bash
# Verificar instalación de pdflatex
pdflatex --version

# Verificar instalación de ImageMagick
convert --version

# Reinstalar dependencias
go clean -modcache
go get -u
```
