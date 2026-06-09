# SurfaceProxy

SurfaceProxy is an ultra-low latency, non-blocking proxy engine written in Go. It sits inline between AI Agent clients and a target browser, intercepting Chrome DevTools Protocol (CDP) frames to prune the DOM and optimize the context returned to LLMs. For more information: https://proxy.sentrysurface.io/

## Core Architecture

```
                      +------------------+
                      |    AI Agent      |
                      +--------+---------+
                               |
                               | (CDP / WebSocket or stdio)
                               v
                      +--------+---------+
                      |   SurfaceProxy   |
                      +---+----+----+----+
                          |    |    |
       +------------------+    |    +-------------------+
       |                       |                        |
       v                       v                        v
+------+------+         +------+------+          +------+------+
| Firewall    |         | Pruning     |          | Diff Engine |
| Evaluator   |         | Engine      |          | State Ring  |
+-------------+         +-------------+          +-------------+
```

## Setup & Running

1. **Local Run**:
   ```bash
   go run ./cmd/surface-proxy --config surface-proxy.json
   ```

2. **Docker Development Sandbox**:
   ```bash
   docker build -f Dockerfile.dev -t surface-proxy-dev .
   docker run -p 8443:8443 surface-proxy-dev
   ```

## License

Business Source License 1.1 (BSL-LICENSE.txt).

## Feedback & Support

For issues, feedback, and inquiries, please contact support@sentrysurface.io or visit https://sentrysurface.io/contact


