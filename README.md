# 🚅 Railtail

Railtail is an HTTP/TCP proxy for Railway workloads connecting to Tailscale
nodes. It listens on a local address and forwards traffic it receives on
the local address to a target Tailscale node address.

It can also serve as a general HTTP proxy to forward connections from external services
into your tailnet.

Features:
- **Dual Protocol Support**: Works with both HTTP and TCP connections
- **Tailnet Proxy Mode**: Act as a general HTTP proxy for your entire tailnet without a specific target
- **TLS Configuration**: Configurable TLS certificate validation for HTTPS connections
- **Simple Setup**: Easy to deploy to Railway or run locally
- **Resource Efficient**: Lightweight with minimal resource usage

📣 This is a workaround until there are [full VMs available in Railway](https://help.railway.com/feedback/full-unix-v-ms-44eef294). Please upvote the thread if you want this feature!

## Usage

### Deploying to Railway

1. [Install and setup Tailscale](https://tailscale.com/kb/1017/install) on the
   machine you want to connect to. If you're using Tailscale as a subnet
   router, ensure you advertise the correct routes and approve the subnets
   in the Tailscale admin console.

2. Deploy this service to Railway as a container.

Look at the [Examples](#examples) section for provider-specific examples.

### Running Locally

To run railtail locally, follow these steps:

1. [Install Go](https://go.dev/doc/install) if you haven't already.

2. Clone this repository:
   ```sh
   git clone https://github.com/rmonvfer/railtail.git
   cd railtail
   ```

3. Build the binary:
   ```sh
   go build -o railtail
   ```

4. Create a `.env` file with your configuration:
   ```sh
   TARGET_ADDR=10.0.0.1:3306  # or http://10.0.0.1:8080 for HTTP proxy
   LISTEN_PORT=8000
   TS_HOSTNAME=railtail-local
   TS_AUTH_KEY=tskey-auth-xxxxxxxxxxxx-yyyyyyyyyyyyyyyyyyyyyyy
   # Optional:
   # TS_LOGIN_SERVER=https://headscale.example.com
   # TS_STATEDIR_PATH=/tmp/railtail-local
   # INSECURE_SKIP_VERIFY=false
   ```

5. Run the application:
   ```sh
   # Using environment variables
   export TARGET_ADDR=10.0.0.1:3306
   export LISTEN_PORT=8000
   export TS_HOSTNAME=railtail-local
   export TS_AUTH_KEY=tskey-auth-xxxxxxxxxxxx-yyyyyyyyyyyyyyyyyyyyyyy
   ./railtail
   
   # Or using command line arguments
   ./railtail -target-addr 10.0.0.1:3306 -listen-port 8000 -ts-hostname railtail-local
   ```

6. Connect to your service via localhost:
   ```sh
   # For TCP connections
   nc localhost 8000
   
   # For HTTP connections
   curl http://localhost:8000
   ```

## Configuration

Railtail has three operating modes:

1. **TCP Forwarding Mode**: Set `TARGET_ADDR` to a bare address (like `100.100.100.100:3306`) without a scheme
2. **HTTP Forwarding Mode**: Set `TARGET_ADDR` with `http://` or `https://` scheme (like `http://100.100.100.100:8080`)
3. **Tailnet Proxy Mode**: Set `PROXY_MODE=true` and omit `TARGET_ADDR` to proxy requests to any tailnet host

| Environment Variable   | CLI Argument            | Description                                                                                                                                                   |
|------------------------|-------------------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `TARGET_ADDR`          | `-target-addr`          | Required when not in proxy mode. Address of the Tailscale node to send traffic to. Omit when using `PROXY_MODE=true`.                                         |
| `PROXY_MODE`           | `-proxy-mode`           | Optional. Set to `true` to run as a general tailnet proxy without requiring a specific target address. When enabled, `TARGET_ADDR` is not needed.             |
| `LISTEN_PORT`          | `-listen-port`          | Required. Port to listen on.                                                                                                                                  |
| `TS_HOSTNAME`          | `-ts-hostname`          | Required. Hostname to use for Tailscale.                                                                                                                      |
| `TS_AUTH_KEY`          | N/A                     | Required. Tailscale auth key. Must be set in environment.                                                                                                     |
| `TS_LOGIN_SERVER`      | `-ts-login-server`      | Optional. Base URL of the control server. If you are using Headscale for your control server, use your Headscale instance's url. Defaults to using Tailscale. |
| `TS_STATEDIR_PATH`     | `-ts-state-dir`         | Optional. Tailscale state dir. Defaults to `/tmp/railtail`.                                                                                                   |
| `INSECURE_SKIP_VERIFY` | `-insecure-skip-verify` | Optional. Skip TLS certificate verification when connecting via HTTPS. Defaults to `true`. Set to `false` to enable certificate validation.                   |

_CLI arguments will take precedence over environment variables._

## About

This was created to work around userspace networking restrictions. Dialing a
Tailscale node from a container requires you to do it over Tailscale's
local SOCKS5/HTTP proxy, which is not always ergonomical especially if
you're connecting to databases or other services with minimal support
for SOCKS5 (e.g. db connections from an application).

railtail is designed to be run as a separate service in Railway that you
connect to over Railway's Private Network.

> ⚠️ **Warning**: Do not expose this service on Railway publicly!
>
> ![Networking settings warning](https://res.cloudinary.com/railway/image/upload/v1733851092/cs-2024-12-11-01.12_f1z1xy.png)
>
> This service is intended to be used via Railway's Private Network only.

## Examples

### Connecting to an AWS RDS instance

1. Configure Tailscale on an EC2 instance in the same VPC as your RDS instance:

   ```sh
   # In EC2
   curl -fsSL https://tailscale.com/install.sh | sh

   # Enable IP forwarding
   echo 'net.ipv4.ip_forward = 1' | sudo tee -a /etc/sysctl.d/99-tailscale.conf
   echo 'net.ipv6.conf.all.forwarding = 1' | sudo tee -a /etc/sysctl.d/99-tailscale.conf
   sudo sysctl -p /etc/sysctl.d/99-tailscale.conf

   # Start Tailscale. Follow instructions to authenticate the node if needed,
   # and make sure you approve the subnet routes in the Tailscale admin console
   sudo tailscale up --reset --advertise-routes=172.31.0.0/16
   ```

2. Deploy railtail into your pre-existing Railway project:

   [![Deploy on Railway](https://railway.com/button.svg)](https://railway.com/template/railtail?referralCode=EPXG5z)

3. Use your new railtail service's Private Domain to connect to your RDS instance:

   ```sh
   DATABASE_URL="postgresql://username:password@${{railtail.RAILWAY_PRIVATE_DOMAIN}}:${{railtail.LISTEN_PORT}}/dbname"
   ```

### Connecting to a Private HTTP API

1. Set up Tailscale on the server hosting your private API:

   ```sh
   # On your API server
   curl -fsSL https://tailscale.com/install.sh | sh
   sudo tailscale up
   ```

2. Note the Tailscale IP address of your server:

   ```sh
   tailscale ip -4
   # Example output: 100.100.100.100
   ```

3. Configure railtail with HTTP forwarding:

   ```sh
   # In Railway or local .env file
   TARGET_ADDR=http://100.100.100.100:8080
   LISTEN_PORT=3000
   TS_HOSTNAME=railtail-api
   TS_AUTH_KEY=tskey-auth-xxxxxxxxxxxx-yyyyyyyyyyyyyyyyyyyyyyy
   ```

4. Connect to your API through railtail:

   ```sh
   # If using Railway
   curl http://${{railtail.RAILWAY_PRIVATE_DOMAIN}}:${{railtail.LISTEN_PORT}}/api/endpoint
   
   # If running locally
   curl http://localhost:3000/api/endpoint
   ```

### Using as a General Tailnet Proxy

This mode allows you to use railtail as a general HTTP proxy to access any host in your tailnet without specifying a single target:

1. Start railtail in proxy mode:

   ```sh
   # In Railway or local .env file
   PROXY_MODE=true  # Enable proxy mode (no TARGET_ADDR needed)
   LISTEN_PORT=8080
   TS_HOSTNAME=railtail-proxy
   TS_AUTH_KEY=tskey-auth-xxxxxxxxxxxx-yyyyyyyyyyyyyyyyyyyyyyy
   ```

2. Configure your application or container to use railtail as an HTTP proxy:

   ```sh
   # Using environment variables
   export HTTP_PROXY=http://localhost:8080  # If running locally
   # Or
   export HTTP_PROXY=http://${{railtail.RAILWAY_PRIVATE_DOMAIN}}:${{railtail.LISTEN_PORT}}  # If using Railway
   
   # Same for HTTPS
   export HTTPS_PROXY=http://localhost:8080
   ```

3. Make requests to any tailnet host:

   ```sh
   # The hostname in the URL is used to determine the target
   curl -x http://localhost:8080 http://machine1.ts.net/api/resource
   curl -x http://localhost:8080 http://machine2.ts.net:8443/other/resource
   
   # Or with the proxy environment variables set
   curl http://machine1.ts.net/api/resource
   ```

4. For Docker Compose, configure as a sidecar proxy:

   ```yaml
   services:
     app:
       image: your-app-image
       environment:
         - HTTP_PROXY=http://railtail:8080
         - HTTPS_PROXY=http://railtail:8080
     
     railtail:
       image: railtail
       environment:
         - PROXY_MODE=true
         - LISTEN_PORT=8080
         - TS_HOSTNAME=railtail-proxy
         - TS_AUTH_KEY=tskey-auth-xxxxxxxxxxxx-yyyyyyyyyyyyyyyyyyyyyyy
   ```

## Development

### Building from Source

```sh
# Clone the repository
git clone https://github.com/rmonvfer/railtail.git
cd railtail

# Build the binary
go build -o railtail .

# Run tests
go test ./...
```

### Docker

You can also run railtail using Docker:

```sh
# Build the Docker image
docker build -t railtail .

# Run the container
docker run -p 8000:8000 \
  -e TARGET_ADDR=10.0.0.1:3306 \
  -e LISTEN_PORT=8000 \
  -e TS_HOSTNAME=railtail-docker \
  -e TS_AUTH_KEY=tskey-auth-xxxxxxxxxxxx-yyyyyyyyyyyyyyyyyyyyyyy \
  railtail
```

## Troubleshooting

### Common Issues

1. **Connection Timeout**: If connections timeout, check that:
   - Your Tailscale node is reachable
   - The target service is running on the specified port
   - Your firewall allows the connection
   - The proper subnets are advertised if using a subnet router

2. **Certificate Validation Errors**:
   - For development/testing, set `INSECURE_SKIP_VERIFY=true`
   - For production, ensure your certificates are valid and trusted

3. **Permission Denied Errors**:
   - Ensure the state directory is writable
   - Check that the Tailscale auth key has sufficient permissions

## Security Considerations

- Do not expose the railtail service publicly
- Use Railway's Private Network feature to limit access
- Rotate your Tailscale auth keys periodically
- Consider enabling certificate validation in production environments

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

This project is licensed under the MIT License - see the LICENSE file for details.
