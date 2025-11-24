# Multipath QUIC RL Scheduler

[![DOI](https://img.shields.io/badge/DOI-10.3390/s22176333-important)](https://doi.org/10.3390/s22176333)

**Reinforcement Learning Based Multipath QUIC Scheduler for Multimedia Streaming**
- Seunghwa Lee, and Joon Yoo.
- Sensors 22.17 (2022): 6333.
- [https://doi.org/10.3390/s22176333](https://doi.org/10.3390/s22176333)

## Summary
This project implements a reinforcement learning-based scheduler to optimize the performance of Dynamic Adaptive Streaming over HTTP (DASH) in Multipath QUIC (MPQUIC).

The testbed is constructed based on [MAppLE](https://github.com/vuva/MAppLE). The main components of the MAppLE platform are as follows:
- **Caddy**: A web server supporting QUIC
- **AStream**: Python-based DASH performance measurement framework
- **quic-proxy**: A proxy that processes HTTP requests from the Python DASH client based on quic-go
- **quic-go**: MPQUIC integration in a QUIC-based fork

Here, the quic-go implementing MPQUIC uses the quic-go from the Peekaboo Repository. Therefore, ECF, BLEST, and Peekaboo can be tested in a DASH environment.

Finally, we added a reinforcement learning scheduler implementation that considers chunk information and the client's buffer status.

**References**
  - **MAppLE**: [https://github.com/vuva/MAppLE](https://github.com/vuva/MAppLE)
  - **AStream**: [https://github.com/pari685/AStream](https://github.com/pari685/AStream)
  - **Peekaboo**: [https://ieeexplore.ieee.org/document/9110610](https://ieeexplore.ieee.org/document/9110610)

## Environment Setup
We simplified the experimental environment configuration using Docker, allowing the test environment to be built regardless of the Linux kernel version. (It was tested on Ubuntu 18 in the original paper.)

### 1. Run Docker Compose
You can build and run the containers using the command below.
```bash
docker compose up -d --build
```
This process may take some time as Go Build, video chunk preparation via ffmpeg, and Mininet environment setup are executed simultaneously using multi-stage builds.
### 2. Generate Certificates for HTTPS Testing
Caddy and AStream (Python DASH Client) use certificates to enable HTTPS.

Once the Docker containers are running, you can generate certificates via the following command:
```bash
docker exec -it mininet /certs/makecert.sh
```

Executing this code will create cert.pem and privkey.pem inside the /docker/certs folder.

> \[!NOTE]
>
> **If the server IP has changed due to network topology changes**
> 
> 1. Change the HOST IP entered in `/docker/certs/make_cert.sh`.
> 2. Remove all `/docker/certs/*.pem` files.
> 3. Regenerate the certificates.

## Test Methods

You can proceed with the test using the command below.
```
docker exec -it mininet python dash_demo.py --scheduler rl
```

**Available Scheduler Options**
- rtt
- ecf
- blest
- peek (Peekaboo)
- rl (Our scheduler)

**Execution Results**

The results are saved in /docker/logs.
- `server.logs`: Logs output by Caddy. Since the server handles packet scheduling, if scheduler-related logs are added, they will be recorded in this file.
- `client.logs`: Logs output by AStream.
- `log/*`: Logs processed and saved by AStream. Metrics such as download time per chunk, buffering time, and initial playback delay can be checked.

## Customization
### Network Topology
- **Topology**: You can change the network topology by modifying `/docker/mininettest/basicTopo.py`.
- **Network Properties**: You can configure Packet loss, Bandwidth, Delay, etc., by modifying `tc_client.bash` and `tc_s1.bash` in the scripts subfolder.

### MPQUIC Scheduler
The scheduler is implemented in `/MAppLE/quic-go/scheduler.go`.

For details, you can check the references for each scheduler at `/MAppLE/quic-go/scheduler.go:selectPath`.