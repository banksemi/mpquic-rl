# Multipath QUIC RL Scheduler

[![DOI](https://img.shields.io/badge/DOI-10.3390/s22176333-important)](https://doi.org/10.3390/s22176333)

**Reinforcement Learning Based Multipath QUIC Scheduler for Multimedia Streaming**
- Seunghwa Lee, and Joon Yoo.
- Sensors 22.17 (2022): 6333.
- [https://doi.org/10.3390/s22176333](https://doi.org/10.3390/s22176333)

## 요약
이 프로젝트는 Multipath QUIC(MPQUIC)에서 Dynamic Adaptive Streaming over HTTP(DASH)의 성능을 최적화하기 위한 강화학습 기반 스케줄러를 구현합니다.

테스트베드는 [MAppLE](https://github.com/vuva/MAppLE)을 기반으로 구성되었습니다. MAppLE 플랫폼의 주요 구성 요소는 다음과 같습니다.
- **Caddy**: QUIC을 지원하는 웹서버
- **AStream**: Python 기반 DASH 성능 측정 프레임워크
- **quic-proxy**: 파이썬 DASH 클라이언트의 HTTP 요청을 quic-go 기반으로 처리하는 프록시
- **quic-go**: QUIC 기반 포크에서 MPQUIC 통합

여기에서 MPQUIC을 구현하는 quic-go는 Peekaboo Repository의 quic-go를 사용합니다. 따라서 DASH 환경에서 ECF, BLEST, Peekaboo를 테스트할 수 있습니다.

마지막으로 청크 정보와 클라이언트의 버퍼 상태를 고려하는 강화학습 스케줄러 구현을 추가했습니다.

**References**
  - **MAppLE**: [https://github.com/vuva/MAppLE](https://github.com/vuva/MAppLE)
  - **AStream**: [https://github.com/pari685/AStream](https://github.com/pari685/AStream)
  - **Peekaboo**: [https://ieeexplore.ieee.org/document/9110610](https://ieeexplore.ieee.org/document/9110610)

## 실험 환경 구성

Docker를 사용하여 실험 환경 구성을 단순화했으며, 리눅스 커널 버전에 관계없이 테스트 환경을 구축할 수 있습니다. (원 논문에서는 Ubuntu 18 버전에서 테스트되었습니다.)
### 1. Docker Compose 실행
아래 명령어를 사용하여 컨테이너를 빌드하고 실행할 수 있습니다.
```bash
docker compose up -d --build
```
이 과정에서 멀티스테이지 빌드를 사용하여 Go Build, ffmpeg를 통한 비디오 청크 준비, Mininet 환경 구축이 동시에 실행되므로 약간의 시간이 필요할 수 있습니다.

### 2. HTTPS 테스트를 위한 인증서 생성
Caddy와 AStream (Python DASH Client)는 인증서를 사용하여 HTTPS를 활성화합니다.

도커 컨테이너가 실행되면 다음 명령을 통해 인증서를 생성할 수 있습니다.
```bash
docker exec -it mininet /certs/makecert.sh
```

이 코드를 실행하면 `/docker/certs` 폴더 내에 `cert.pem`, `privkey.pem`이 생성됩니다.
> \[!NOTE]
>
> **네트워크 토플리지 변경으로 서버 IP가 변경된 경우**
>
> 1. `/docker/certs/make_cert.sh`에 입력된 HOST IP를 변경하세요.
> 2. `/docker/certs/*.pem` 파일을 모두 제거하세요.
> 3. 인증서를 다시 생성하세요.


## 테스트 방법

아래 명령을 사용하여 테스트를 진행할 수 있습니다.
```
docker exec -it mininet python dash_demo.py --scheduler rl
```

**사용 가능한 스케줄러 옵션**
- rtt
- ecf
- blest
- peek (Peekaboo)
- rl (Our scheduler)

**실행 결과**

결과는 /docker/logs에 저장됩니다.
- `server.logs`: Caddy에서 출력하는 로그입니다. 서버가 패킷 스케줄링을 담당하므로, 스케줄러 관련 로그를 추가하면 이 파일에 기록됩니다.
- `client.logs`: AStream에서 출력한 로그입니다.
- `log/*`: AStream이 가공하여 저장한 로그입니다. 청크별 다운로드 시간, 버퍼링 시간, 초기 재생 지연 등의 지표를 확인할 수 있습니다.

## 코드 커스터마이징
### 네트워크 토폴로지
- **토폴로지**: `/docker/mininettest/basicTopo.py` 을 수정하여 네트워크 토플리지를 변경할 수 있습니다.
- **네트워크 속성**: 하위 폴더인 scripts의 `tc_client.bash`, `tc_s1.bash`을 수정하여 Packet loss, Bandwidth, Delay 등을 설정할 수 있습니다.

### MPQUIC 스케줄러
스케줄러는 `/MAppLE/quic-go/scheduler.go`에 구현되어있습니다.

자세한 내용은 `/MAppLE/quic-go/scheduler.go:selectPath` 에서 각 스케줄러의 참조를 확인할 수 있습니다.
