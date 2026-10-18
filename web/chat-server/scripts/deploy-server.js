/**
 * 前端本地部署服务器（零依赖，只用 Node 内置模块）
 *
 * 职责三件事：
 * 1. 把 dist/ 作为静态站点在 5000 端口提供出去，SPA 路由回落到 index.html；
 * 2. 把后端接口路径（/login、/user/...、/static/... 等）反向代理到 8080；
 * 3. 把 WebSocket 升级请求（/wss）透传到 8080。
 *
 * 这样前端 store 里 "backendUrl = 当前页面 origin" 的约定不需要改，
 * 页面在 5000，所有请求仍走 5000，由本服务器转发到后端。
 */
const http = require("http");
const net = require("net");
const fs = require("fs");
const path = require("path");

const PORT = Number(process.env.PORT || 5000);
const BACKEND_HOST = process.env.BACKEND_HOST || "127.0.0.1";
const BACKEND_PORT = Number(process.env.BACKEND_PORT || 8080);
const ROOT = path.resolve(__dirname, "..", "dist");

// 需要转发给后端的路径前缀
const PROXY_PREFIXES = [
  "/login",
  "/register",
  "/auth/",
  "/user",
  "/group",
  "/session",
  "/contact",
  "/message",
  "/chatroom",
  "/dlq",
  "/bench",
  "/static/",
  "/readyz",
  "/metrics",
  "/debug/",
  "/wss",
];

const MIME = {
  ".html": "text/html; charset=utf-8",
  ".js": "application/javascript; charset=utf-8",
  ".css": "text/css; charset=utf-8",
  ".json": "application/json; charset=utf-8",
  ".svg": "image/svg+xml",
  ".png": "image/png",
  ".jpg": "image/jpeg",
  ".jpeg": "image/jpeg",
  ".gif": "image/gif",
  ".webp": "image/webp",
  ".ico": "image/x-icon",
  ".woff": "font/woff",
  ".woff2": "font/woff2",
  ".ttf": "font/ttf",
  ".map": "application/json; charset=utf-8",
};

const shouldProxy = (url) =>
  PROXY_PREFIXES.some((prefix) => url === prefix || url.startsWith(prefix));

const serveStatic = (req, res) => {
  const urlPath = decodeURIComponent((req.url || "/").split("?")[0]);
  let filePath = path.join(ROOT, urlPath);

  // 阻止路径穿越
  if (!filePath.startsWith(ROOT)) {
    res.writeHead(403).end("forbidden");
    return;
  }

  fs.stat(filePath, (err, stat) => {
    if (err || stat.isDirectory()) {
      // SPA 回落
      filePath = path.join(ROOT, "index.html");
    }
    fs.readFile(filePath, (readErr, content) => {
      if (readErr) {
        res.writeHead(404, { "Content-Type": "text/plain; charset=utf-8" });
        res.end("not found");
        return;
      }
      const ext = path.extname(filePath).toLowerCase();
      res.writeHead(200, {
        "Content-Type": MIME[ext] || "application/octet-stream",
        "Cache-Control": ext === ".html" ? "no-cache" : "public, max-age=604800",
      });
      res.end(content);
    });
  });
};

const proxyHttp = (req, res) => {
  const options = {
    host: BACKEND_HOST,
    port: BACKEND_PORT,
    method: req.method,
    path: req.url,
    headers: { ...req.headers, host: `${BACKEND_HOST}:${BACKEND_PORT}` },
  };
  const upstream = http.request(options, (upstreamRes) => {
    res.writeHead(upstreamRes.statusCode || 502, upstreamRes.headers);
    upstreamRes.pipe(res);
  });
  upstream.on("error", (error) => {
    if (!res.headersSent) {
      res.writeHead(502, { "Content-Type": "application/json; charset=utf-8" });
    }
    res.end(
      JSON.stringify({ code: 502, message: `后端不可用: ${error.message}` })
    );
  });
  req.pipe(upstream);
};

const server = http.createServer((req, res) => {
  if (shouldProxy(req.url || "")) {
    proxyHttp(req, res);
    return;
  }
  serveStatic(req, res);
});

// WebSocket 透传：手工拼请求行与头部后双向 pipe。
server.on("upgrade", (req, socket, head) => {
  const upstream = net.connect(BACKEND_PORT, BACKEND_HOST, () => {
    upstream.write(`${req.method} ${req.url} HTTP/1.1\r\n`);
    for (const [key, value] of Object.entries(req.headers)) {
      if (Array.isArray(value)) {
        value.forEach((item) => upstream.write(`${key}: ${item}\r\n`));
      } else {
        upstream.write(`${key}: ${value}\r\n`);
      }
    }
    upstream.write("\r\n");
    if (head && head.length) upstream.write(head);
    upstream.pipe(socket);
    socket.pipe(upstream);
  });

  upstream.on("error", () => socket.destroy());
  socket.on("error", () => upstream.destroy());
});

server.listen(PORT, "0.0.0.0", () => {
  console.log(`[frontend] http://127.0.0.1:${PORT}  ->  后端 ${BACKEND_HOST}:${BACKEND_PORT}`);
  console.log(`[frontend] 静态目录 ${ROOT}`);
});
