# Media Processor

Microservicio HTTP (Go) que **optimiza imágenes y videos**: achica, convierte a
WebP / MP4 y devuelve el archivo listo para guardar.

**No guarda nada.** Recibe un archivo, lo procesa en memoria y responde con el
binario resultante. Dónde se almacena (S3, Contabo, MinIO, disco propio, etc.)
lo decide **tu sistema**: el servicio no conoce tu bucket ni pide sus claves.

```
 Usuario ──archivo──▶ TU BACKEND ──POST multipart──▶ MEDIA PROCESSOR
                          │   ◀──binario optimizado + cabeceras X-*──┘
                          │
                          └──▶ tu bucket / tu servidor (lo subes tú)
```

- [1. Formas de usarlo](#1-formas-de-usarlo)
- [2. Autenticación](#2-autenticación)
- [3. Referencia del API](#3-referencia-del-api)
- [4. Errores](#4-errores)
- [5. Límites y comportamiento](#5-límites-y-comportamiento)
- [6. Receta de integración](#6-receta-de-integración)
- [7. Ejemplos de código](#7-ejemplos-de-código)
- [8. Instalarlo en tu servidor](#8-instalarlo-en-tu-servidor)
- [9. Lista de verificación](#9-lista-de-verificación)

---

## 1. Formas de usarlo

Se instala **en tu propio servidor**, como un contenedor Docker al lado de tu
backend (§8). Tus archivos nunca salen de tu red y la capacidad es toda tuya.

Orden sugerido: instalar (§8) → probar con `curl` (§3) → integrar en tu backend
(§6 y §7) → repasar la lista (§9).

En esta guía `$MP_URL` es la URL base del servicio (normalmente
`http://media-processor:4000`) y `$MP_KEY` la API key que tú defines.

> Llama al servicio **desde tu backend**, no desde el navegador ni desde la app:
> la API key no debe viajar al cliente.

---

## 2. Autenticación

Si el servicio tiene `API_KEY` configurada, **todas** las rutas la exigen
(también `/health` y `/presets`). Va en cualquiera de estas dos cabeceras:

```
X-API-Key: <tu-api-key>
Authorization: Bearer <tu-api-key>
```

Sin clave o con clave incorrecta: `401 UNAUTHORIZED`.

Si `API_KEY` está vacía el servicio acepta todo. No lo dejes así: define
siempre una clave (§8, paso 2).

---

## 3. Referencia del API

Los tres endpoints de proceso reciben `multipart/form-data` y, cuando salen
bien, responden **`200` con el archivo en el cuerpo** (binario, no JSON). Los
datos del resultado van en cabeceras `X-*`. Los errores sí son JSON (ver §4).

En todos, la parte `file` debe traer su `Content-Type` real (`image/jpeg`,
`video/mp4`, ...): el servicio lo usa para validar.

Campo opcional común: `empresaId` — un identificador estable de tu sistema o
de tu cliente; se usa para el límite de frecuencia (§5).

### 3.1 `POST /process/image`

Achica y convierte una imagen.

| Campo | Tipo | Obligatorio | Descripción |
|---|---|---|---|
| `file` | archivo | sí | JPEG, PNG, GIF, BMP o TIFF. Máx. 10 MB |
| `preset` | texto | no | Ver tabla. Por defecto `default`. Un nombre desconocido usa `default` sin avisar |
| `width` | entero | no | Ancho máximo en px (reemplaza el del preset) |
| `height` | entero | no | Alto máximo en px (reemplaza el del preset) |
| `quality` | 1-100 | no | Calidad (reemplaza la del preset) |
| `format` | texto | no | `webp` (por defecto), `jpeg` o `png` |

Presets (`GET /presets` los devuelve en JSON):

| Preset | Máx. ancho × alto | Calidad | Uso sugerido |
|---|---|---|---|
| `thumbnail` | 300 × 300 | 75 | Miniaturas de listas |
| `profile` | 500 × 500 | 85 | Foto de perfil |
| `logo` | 512 × 512 | 90 | Logos |
| `medium` | 800 × 800 | 80 | Tarjetas, vistas intermedias |
| `producto` | 1200 × 1200 | 85 | Foto principal de producto |
| `galeria` | 1600 × 1600 | 82 | Galería a pantalla completa |
| `default` | 1600 × 1600 | 80 | Cualquier otra cosa |
| `banner` | 1920 × 720 | 80 | Banners |

La imagen se ajusta **dentro** de esas medidas respetando su proporción (no
recorta, no deforma) y **nunca se agranda**: si ya es más chica, solo se
recodifica.

Cabeceras de la respuesta:

| Cabecera | Ejemplo | Significado |
|---|---|---|
| `Content-Type` | `image/webp` | Tipo real del resultado: **úsalo para la extensión** |
| `X-Format` | `webp` | `webp`, `jpeg`, `png` (o el formato original, ver nota) |
| `X-Width` / `X-Height` | `1200` / `900` | Medidas finales en px |
| `X-Original-Size` | `2483921` | Bytes recibidos |
| `X-Processed-Size` | `184220` | Bytes devueltos |
| `X-Reduction` | `92.6%` | Ahorro |
| `X-Duration` | `1.42s` | Tiempo de proceso |

> **El formato de salida no está garantizado.** Si pides `jpeg` o `png` y el
> resultado pesa más que el original, se devuelve el **original sin tocar**
> (con su tipo y medidas originales). Y si el servicio corre fuera de Docker
> sin `cwebp` instalado, lo que pidas en WebP sale en JPEG. Guarda siempre con
> la extensión que indique `Content-Type`, no con la que pediste.

```bash
curl -X POST "$MP_URL/process/image" \
  -H "X-API-Key: $MP_KEY" \
  -F "file=@foto.jpg;type=image/jpeg" \
  -F "preset=producto" \
  -F "empresaId=mi-sistema" \
  -D cabeceras.txt -o foto.webp
```

### 3.2 `POST /process/video`

Comprime un video a **MP4 (H.264 + AAC)** con arranque rápido (faststart: el
video empieza a reproducirse sin descargarse entero).

| Campo | Tipo | Obligatorio | Descripción |
|---|---|---|---|
| `file` | archivo | sí | `video/mp4`, `video/quicktime`, `video/webm`, `video/x-msvideo`. Máx. 200 MB |
| `maxWidth` | entero | no | Ancho máximo en px. Por defecto `1280`. No agranda |

Cabeceras de la respuesta:

| Cabecera | Significado |
|---|---|
| `Content-Type` | `video/mp4` (o el tipo original si `X-Skipped: true`) |
| `X-Skipped` | `true` = comprimir no redujo el tamaño: **el cuerpo es tu archivo original**. En ese caso no vienen las demás cabeceras `X-*` salvo `X-Duration` |
| `X-Width` / `X-Height` | Medidas finales |
| `X-Video-Duration` | Duración del video en segundos (`15.0`) |
| `X-Original-Size` / `X-Processed-Size` / `X-Reduction` | Como en imágenes |
| `X-Duration` | Tiempo de proceso |

```bash
curl -X POST "$MP_URL/process/video" \
  -H "X-API-Key: $MP_KEY" \
  -F "file=@clip.mp4;type=video/mp4" \
  -F "maxWidth=1280" \
  --max-time 130 -D cabeceras.txt -o clip-optimizado.mp4
```

### 3.3 `POST /process/video/thumbnail`

Devuelve una **imagen WebP** con un cuadro del video (el del segundo 1; si el
video es más corto, el primero).

| Campo | Tipo | Obligatorio | Descripción |
|---|---|---|---|
| `file` | archivo | sí | Video, mismos tipos y tope que 3.2 |
| `width` | entero | no | Ancho máximo. Por defecto `480` |
| `height` | entero | no | Alto máximo. Por defecto `360` |

Cabeceras: `Content-Type: image/webp`, `X-Video-Width`, `X-Video-Height` y
`X-Video-Duration` (datos **del video**, no de la miniatura), `X-Duration`.

```bash
curl -X POST "$MP_URL/process/video/thumbnail" \
  -H "X-API-Key: $MP_KEY" \
  -F "file=@clip.mp4;type=video/mp4" \
  -o miniatura.webp
```

### 3.4 `GET /health`

```json
{
  "status": "ok",
  "time": "2026-11-21T10:15:00Z",
  "workers": { "workers": 3, "active": 1, "queued": 0, "queueMax": 500, "processed": 1284, "failed": 2 }
}
```

`queued` alto de forma sostenida = el servicio está saturado.

### 3.5 `GET /presets`

Devuelve los presets de §3.1 en JSON.

---

## 4. Errores

Cuerpo JSON: `{ "error": "mensaje legible", "code": "CODIGO" }`.

| HTTP | `code` | Causa | Qué hacer |
|---|---|---|---|
| 400 | `NO_FILE` | Falta la parte `file` | Corregir la llamada |
| 400 | `INVALID_TYPE` | El `Content-Type` de `file` no es `image/*` o `video/*` | Enviar el tipo real del archivo |
| 400 | `CORRUPT_FILE` | No es una imagen reconocible (incluye HEIC/HEIF y SVG) | Rechazar o guardar el original |
| 400 | `FILE_TOO_LARGE` | Supera el tope | Rechazar antes en tu backend |
| 401 | `UNAUTHORIZED` | API key ausente o incorrecta | Revisar la clave |
| 405 | `METHOD_NOT_ALLOWED` | No es `POST` | Corregir la llamada |
| 429 | `RATE_LIMIT` | Más de 60 solicitudes por minuto | Reintentar tras unos segundos |
| 500 | `PROCESS_ERROR` | No se pudo procesar (formato no soportado, archivo dañado) | Guardar el original |
| 503 | `QUEUE_FULL` | Cola llena | Reintentar con espera, o guardar el original |
| 504 | `TIMEOUT` | El proceso tardó demasiado | Guardar el original |

---

## 5. Límites y comportamiento

Leer antes de integrar: estas son las condiciones reales del servicio.

**Formatos de imagen**
- Entrada soportada: **JPEG, PNG, GIF, BMP, TIFF**.
- **WebP de entrada no se procesa** (responde `500 PROCESS_ERROR`): si el
  archivo ya es WebP, guárdalo tal cual.
- **HEIC/HEIF** (fotos de iPhone sin convertir) y **SVG**: `400 CORRUPT_FILE`.
- **GIF animado**: sale una imagen fija (primer cuadro).
- **Orientación EXIF**: no se aplica y los metadatos se eliminan. Una foto de
  celular que dependa de la etiqueta de orientación puede salir girada: endereza
  la imagen antes de enviarla (la mayoría de las librerías de cámara/selector
  de imágenes ya lo hacen al comprimir).
- La transparencia se conserva en WebP y PNG; en JPEG se pierde.

**Tamaños**
- Imagen: 10 MB. Video: 200 MB. Configurables al instalar (§8).
- Valida el tamaño **en tu backend** antes de llamar; no dependas de que el
  servicio lo rechace.

**Tiempos** (referencia medida con 4 CPU)
- Foto de celular: 2-3 s. Captura PNG: ~0,5 s. Miniatura de video: ~1 s.
- Video de 15 s en 1080p: ~10 s.
- Capacidad: ~1,5 fotos por segundo sostenidas; por encima se forma cola.
- **La conexión se corta a los 120 s** (subida + proceso + descarga). Un video
  largo no entra en ese tiempo: limita la duración o el peso de los videos que
  aceptas. Usa en tu cliente HTTP un tope de **60 s para imágenes y 130 s para
  videos**.

**Frecuencia**
- 60 solicitudes por minuto por `empresaId`. Envía siempre ese campo con un
  valor fijo; sin él el límite no es fiable.
- Una subida que pide tres tamaños cuenta como tres solicitudes.

**Otros**
- Sin estado: no hay ids de trabajo, ni consulta posterior, ni webhooks. La
  respuesta de la llamada **es** el resultado.
- Si cierras la conexión, el trabajo igual termina de procesarse en el
  servidor (no reintentes en ráfaga).
- El servicio no revisa el contenido (ni antivirus ni moderación).

---

## 6. Receta de integración

Lo que hace tu backend al recibir un archivo de un usuario:

1. **Validar** tipo y tamaño (y que el usuario tenga permiso de subir).
2. **Llamar** al media processor. Para una imagen, lo habitual es pedir varios
   tamaños **en paralelo** con el mismo archivo:
   - el principal (`producto`, `galeria`, `banner`, ...),
   - `medium` para tarjetas,
   - `thumbnail` para listas.
3. **Guardar** cada resultado en tu almacenamiento, con:
   - un nombre **generado por ti** (UUID), nunca el nombre que mandó el usuario;
   - la extensión según el `Content-Type` de la respuesta;
   - ese mismo `Content-Type` como tipo del objeto.
4. **Registrar** en tu base las URLs / claves y, si te sirven, `X-Width`,
   `X-Height` y `X-Processed-Size`.
5. **Si el servicio falla** (sin conexión, `5xx`, `429`, tiempo agotado):
   guarda el **original** y sigue. Una subida nunca debería fallar porque el
   optimizador no respondió.

Para un video: `POST /process/video` + `POST /process/video/thumbnail` en
paralelo; si viene `X-Skipped: true`, guarda tu archivo original.

Sugerencia de nombres en el bucket:

```
<tu-prefijo>/<año>/<mes>/<uuid>.webp          principal
<tu-prefijo>/<año>/<mes>/<uuid>_medium.webp
<tu-prefijo>/<año>/<mes>/<uuid>_thumb.webp
```

Como el nombre es único, puedes servirlos con
`Cache-Control: public, max-age=31536000, immutable`.

---

## 7. Ejemplos de código

Todos hacen lo mismo: optimizar y devolver `{ datos, contentType, ancho, alto }`,
o `null` si el servicio no pudo (para que guardes el original).

### Node.js 18+ (sin dependencias)

```js
const MP_URL = process.env.MEDIA_PROCESSOR_URL; // p. ej. http://media-processor:4000
const MP_KEY = process.env.MEDIA_PROCESSOR_KEY;

const EXT = { 'image/webp': 'webp', 'image/jpeg': 'jpg', 'image/png': 'png', 'image/gif': 'gif' };

async function optimizarImagen(buffer, nombre, mime, preset = 'default') {
  const form = new FormData();
  form.append('file', new Blob([buffer], { type: mime }), nombre);
  form.append('preset', preset);
  form.append('empresaId', 'mi-sistema');

  try {
    const res = await fetch(`${MP_URL}/process/image`, {
      method: 'POST',
      headers: { 'X-API-Key': MP_KEY },
      body: form,
      signal: AbortSignal.timeout(60_000),
    });
    if (!res.ok) {
      console.warn('media-processor:', res.status, await res.text());
      return null;
    }
    const contentType = res.headers.get('content-type');
    return {
      datos: Buffer.from(await res.arrayBuffer()),
      contentType,
      extension: EXT[contentType] ?? 'bin',
      ancho: Number(res.headers.get('x-width')),
      alto: Number(res.headers.get('x-height')),
    };
  } catch (e) {
    console.warn('media-processor no disponible:', e.message);
    return null;
  }
}

// Uso: tres tamaños en paralelo y a tu almacenamiento
async function subirFoto(archivo /* { buffer, originalname, mimetype } */) {
  const [principal, medium, thumb] = await Promise.all(
    ['producto', 'medium', 'thumbnail'].map((p) =>
      optimizarImagen(archivo.buffer, archivo.originalname, archivo.mimetype, p)),
  );
  const id = crypto.randomUUID();
  if (!principal) {
    // Respaldo: guardar el original
    return guardar(`${id}-original`, archivo.buffer, archivo.mimetype);
  }
  await guardar(`${id}.${principal.extension}`, principal.datos, principal.contentType);
  if (medium) await guardar(`${id}_medium.${medium.extension}`, medium.datos, medium.contentType);
  if (thumb) await guardar(`${id}_thumb.${thumb.extension}`, thumb.datos, thumb.contentType);
}
```

`guardar` es tu función. Dos versiones típicas:

```js
// a) Bucket compatible con S3 (AWS, Contabo, MinIO, R2, Wasabi...)
import { S3Client, PutObjectCommand } from '@aws-sdk/client-s3';
const s3 = new S3Client({
  region: process.env.S3_REGION,
  endpoint: process.env.S3_ENDPOINT,   // omitir en AWS
  forcePathStyle: true,                // necesario en MinIO / Contabo
  credentials: { accessKeyId: process.env.S3_KEY, secretAccessKey: process.env.S3_SECRET },
});
const guardar = (clave, datos, contentType) =>
  s3.send(new PutObjectCommand({
    Bucket: process.env.S3_BUCKET, Key: clave, Body: datos, ContentType: contentType,
    CacheControl: 'public, max-age=31536000, immutable',
  }));

// b) Disco del propio servidor (servido luego por Nginx o tu framework)
import { writeFile, mkdir } from 'node:fs/promises';
import path from 'node:path';
const RAIZ = '/var/www/media';
async function guardar(clave, datos) {
  const destino = path.join(RAIZ, clave);   // `clave` la generas tú: nunca viene del usuario
  await mkdir(path.dirname(destino), { recursive: true });
  await writeFile(destino, datos);
}
```

### PHP (cURL)

```php
function optimizarImagen(string $ruta, string $mime, string $preset = 'default'): ?array {
    $cabeceras = [];
    $ch = curl_init(getenv('MEDIA_PROCESSOR_URL') . '/process/image');
    curl_setopt_array($ch, [
        CURLOPT_POST => true,
        CURLOPT_RETURNTRANSFER => true,
        CURLOPT_TIMEOUT => 60,
        CURLOPT_HTTPHEADER => ['X-API-Key: ' . getenv('MEDIA_PROCESSOR_KEY')],
        CURLOPT_POSTFIELDS => [
            'file' => new CURLFile($ruta, $mime, basename($ruta)),
            'preset' => $preset,
            'empresaId' => 'mi-sistema',
        ],
        CURLOPT_HEADERFUNCTION => function ($ch, $linea) use (&$cabeceras) {
            $p = explode(':', $linea, 2);
            if (count($p) === 2) $cabeceras[strtolower(trim($p[0]))] = trim($p[1]);
            return strlen($linea);
        },
    ]);
    $cuerpo = curl_exec($ch);
    $estado = curl_getinfo($ch, CURLINFO_HTTP_CODE);
    curl_close($ch);

    if ($cuerpo === false || $estado !== 200) return null;   // guardar el original
    return [
        'datos' => $cuerpo,
        'contentType' => $cabeceras['content-type'] ?? 'application/octet-stream',
        'ancho' => (int)($cabeceras['x-width'] ?? 0),
        'alto' => (int)($cabeceras['x-height'] ?? 0),
    ];
}
```

### Python (requests)

```python
import os, requests

def optimizar_imagen(datos: bytes, nombre: str, mime: str, preset: str = "default"):
    try:
        r = requests.post(
            f"{os.environ['MEDIA_PROCESSOR_URL']}/process/image",
            headers={"X-API-Key": os.environ["MEDIA_PROCESSOR_KEY"]},
            files={"file": (nombre, datos, mime)},
            data={"preset": preset, "empresaId": "mi-sistema"},
            timeout=60,
        )
    except requests.RequestException:
        return None                      # guardar el original
    if r.status_code != 200:
        return None
    return {
        "datos": r.content,
        "content_type": r.headers["Content-Type"],
        "ancho": int(r.headers.get("X-Width", 0)),
        "alto": int(r.headers.get("X-Height", 0)),
    }
```

### Video (Node.js)

```js
async function optimizarVideo(buffer, nombre, mime) {
  const form = new FormData();
  form.append('file', new Blob([buffer], { type: mime }), nombre);
  form.append('maxWidth', '1280');
  form.append('empresaId', 'mi-sistema');

  const res = await fetch(`${MP_URL}/process/video`, {
    method: 'POST',
    headers: { 'X-API-Key': MP_KEY },
    body: form,
    signal: AbortSignal.timeout(130_000),
  }).catch(() => null);

  if (!res?.ok || res.headers.get('x-skipped') === 'true') return null; // usar el original
  return {
    datos: Buffer.from(await res.arrayBuffer()),
    contentType: 'video/mp4',
    duracionSeg: Number(res.headers.get('x-video-duration')),
  };
}
```

---

## 8. Instalarlo en tu servidor

**Requisitos**
- Servidor Linux de 64 bits (x86-64) con Docker y el plugin `docker compose`.
- Mínimo 2 CPU y 2 GB de RAM libres; recomendado 4 CPU y 4 GB.
- Nada más: la imagen ya trae `ffmpeg` y `cwebp`.

**Paso 1. Crear la carpeta**

```bash
mkdir -p /opt/media-processor && cd /opt/media-processor
```

**Paso 2. Generar tu API key**

```bash
openssl rand -hex 32
```

Guárdala: va en el paso 3 y en la configuración de tu backend.

**Paso 3. Crear `stack.env`**

```env
PORT=4000
API_KEY=pega-aqui-la-clave-del-paso-2
MAX_WORKERS=3
QUEUE_SIZE=500
MAX_IMAGE_SIZE_MB=10
MAX_VIDEO_SIZE_MB=200
PROCESSING_TIMEOUT=30
TEMP_CLEANUP_MINS=5
```

```bash
chmod 600 stack.env
```

`MAX_WORKERS` = los CPU que le das al contenedor menos uno (con 4 CPU → 3;
con 2 CPU → 1).

| Variable | Por defecto | Descripción |
|---|---|---|
| `PORT` | `4000` | Puerto HTTP |
| `API_KEY` | vacío | Clave exigida en todas las rutas. **Vacío = sin autenticación** |
| `MAX_WORKERS` | CPUs − 1 | Trabajos simultáneos. Fíjalo a mano: dentro de Docker el valor automático cuenta los CPU del host, no los del contenedor |
| `QUEUE_SIZE` | `500` | Trabajos en espera antes de responder `503 QUEUE_FULL` |
| `MAX_IMAGE_SIZE_MB` | `10` | Tope de imagen |
| `MAX_VIDEO_SIZE_MB` | `200` | Tope de video |
| `PROCESSING_TIMEOUT` | `30` | Segundos por trabajo |
| `TEMP_CLEANUP_MINS` | `5` | Cada cuánto se borran temporales huérfanos |

**Paso 4. Crear `docker-compose.yml`**

Elige según dónde corre tu backend.

*a) Tu backend corre en Docker* (lo más común). Averigua el nombre de su red
con `docker network ls` y ponlo abajo:

```yaml
services:
  media-processor:
    image: ghcr.io/hellares/media-processor:1.0.0
    container_name: media-processor
    restart: unless-stopped
    env_file: stack.env
    networks: [backend]
    deploy:
      resources:
        limits: { cpus: '4', memory: 4G }
    logging:
      driver: json-file
      options: { max-size: 10m, max-file: "3" }

networks:
  backend:
    external: true
    name: NOMBRE-DE-LA-RED-DE-TU-BACKEND
```

Tu backend lo llamará en `http://media-processor:4000`. No se publica ningún
puerto.

*b) Tu backend corre directo en el servidor* (sin Docker):

```yaml
services:
  media-processor:
    image: ghcr.io/hellares/media-processor:1.0.0
    container_name: media-processor
    restart: unless-stopped
    env_file: stack.env
    ports: ["127.0.0.1:4000:4000"]
    deploy:
      resources:
        limits: { cpus: '4', memory: 4G }
    logging:
      driver: json-file
      options: { max-size: 10m, max-file: "3" }
```

Tu backend lo llamará en `http://127.0.0.1:4000`. El `127.0.0.1:` delante del
puerto es lo que impide que quede abierto a internet: no lo quites.

**Paso 5. Levantar y comprobar**

```bash
docker compose up -d
docker ps --filter name=media-processor     # a los ~30 s debe decir (healthy)
docker logs media-processor                 # "Media Processor ready"
```

Prueba desde el servidor (en el caso *a* el puerto no está publicado: se prueba
desde dentro del contenedor):

```bash
# caso a)
docker exec media-processor wget -qO- --header "X-API-Key: TU_CLAVE" http://localhost:4000/health
# caso b)
curl -H "X-API-Key: TU_CLAVE" http://127.0.0.1:4000/health
```

Debe responder `{"status":"ok", ...}`. Sin la cabecera debe responder `401`.

**Paso 6. Configurar tu backend**

Agrega a sus variables de entorno y reinícialo:

```env
MEDIA_PROCESSOR_URL=http://media-processor:4000    # caso b: http://127.0.0.1:4000
MEDIA_PROCESSOR_KEY=la-clave-del-paso-2
```

Luego sigue con §6 y §7.

**Actualizar a una versión nueva**: cambia la etiqueta de `image:` y ejecuta
`docker compose pull && docker compose up -d`. Si la versión nueva no arranca,
vuelve a poner la etiqueta anterior y repite. El corte dura segundos y, si tu
integración guarda el original cuando el servicio falla (§6), nadie lo nota.

**Si algo no anda**

| Síntoma | Causa habitual |
|---|---|
| `docker compose up` dice `network ... not found` | El nombre de la red del paso 4 no existe: `docker network ls` |
| `pull access denied` / `manifest unknown` | Etiqueta mal escrita o imagen de otra arquitectura (solo hay x86-64) |
| Tu backend: `ECONNREFUSED` o `getaddrinfo ENOTFOUND media-processor` | El backend no está en la misma red Docker (caso a) o usa la URL del otro caso |
| `401 UNAUTHORIZED` | La clave del backend no es la de `stack.env` (ojo con espacios o comillas) |
| Todo sale en JPEG en vez de WebP | Estás corriendo el binario fuera de la imagen Docker |
| Respuestas lentas, `queued` alto en `/health` | Faltan CPU, o `MAX_WORKERS` es mayor que los CPU asignados |

**Seguridad de la instalación**
- **No publiques el puerto 4000 a internet.** Tu backend lo alcanza por la red
  interna de Docker (`http://media-processor:4000`) o por `127.0.0.1`.
- Si tiene que ser accesible desde fuera, ponlo detrás de un proxy con HTTPS
  (Nginx, Caddy, Traefik) **y** con `API_KEY` definida. En el proxy sube el
  tope de cuerpo (`client_max_body_size 200m;` en Nginx) y los tiempos de
  espera a 130 s.
- El servicio responde con CORS abierto (`*`). Eso no lo hace seguro para
  llamarlo desde un navegador: la clave quedaría a la vista.

**Dimensionamiento**: el trabajo es puro CPU. Con 4 CPU: ~90 fotos por minuto,
o 2 videos cortos a la vez. La memoria sube con el tamaño de los archivos
(cada trabajo mantiene el archivo en RAM): calcula 1 GB de base más ~3 veces
el video más grande por trabajo simultáneo.

---

## 9. Lista de verificación

- [ ] `GET /health` responde `200` desde el servidor de tu backend.
- [ ] La API key está en una variable de entorno del backend, no en el cliente.
- [ ] Validas tipo y tamaño antes de llamar.
- [ ] Envías `empresaId` con un valor fijo.
- [ ] La extensión y el tipo del objeto guardado salen del `Content-Type` de la respuesta.
- [ ] El nombre del archivo guardado lo generas tú (UUID).
- [ ] Topes de espera: 60 s imágenes, 130 s videos.
- [ ] Si el servicio falla, guardas el original y la subida no se cae.
- [ ] Manejas `X-Skipped: true` en videos.
- [ ] Los WebP de entrada se guardan sin pasar por el servicio.
- [ ] Probaste con: JPEG de celular, PNG con transparencia, un archivo que no es imagen, y un video.
