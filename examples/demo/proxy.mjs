// Minimal same-origin proxy for the local demo: /api/* -> API, everything
// else -> web panel. In production Caddy does this (see deploy/panel).
// The web panel's CSP only allows API calls to its own origin.
import http from 'node:http'

const API = new URL(process.env.DEMO_API ?? 'http://127.0.0.1:18080')
const WEB = new URL(process.env.DEMO_WEB ?? 'http://127.0.0.1:13000')
const PORT = Number(process.env.DEMO_PORT ?? 14000)

http.createServer((req, res) => {
  const target = req.url.startsWith('/api/') ? API : WEB
  const headers = { ...req.headers, host: target.host, 'x-forwarded-for': req.socket.remoteAddress ?? '' }
  const up = http.request({ host: target.hostname, port: target.port, path: req.url, method: req.method, headers }, (r) => {
    res.writeHead(r.statusCode ?? 502, r.headers)
    r.pipe(res)
  })
  up.on('error', () => { res.writeHead(502); res.end('upstream unavailable') })
  req.pipe(up)
}).listen(PORT, '127.0.0.1', () => console.log(`TalePanel demo: http://localhost:${PORT}`))
