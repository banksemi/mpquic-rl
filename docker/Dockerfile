FROM ubuntu:bionic AS builder
RUN apt-get update && apt-get install -y curl gcc libhdf5-serial-dev
ENV GOLANG_VERSION 1.16.5
RUN curl -LO https://dl.google.com/go/go${GOLANG_VERSION}.linux-amd64.tar.gz && tar -C /usr/local -xzf go${GOLANG_VERSION}.linux-amd64.tar.gz
ENV PATH=$PATH:/usr/local/go/bin

WORKDIR /app
COPY ./MAppLE/caddy ./MAppLE/caddy
COPY ./gold gold
COPY ./MAppLE/gorl ./MAppLE/gorl
COPY ./MAppLE/mint-a6080d464fb57a9330c2124ffb62f3c233e3400e ./MAppLE/mint-a6080d464fb57a9330c2124ffb62f3c233e3400e
COPY ./MAppLE/quic-go ./MAppLE/quic-go
RUN cd MAppLE/caddy/caddy && go build -o caddy_custom

COPY ./MAppLE/proxy_module ./MAppLE/proxy_module
RUN cd MAppLE/proxy_module && go build -o proxy_module.so -buildmode=c-shared proxy_module.go

FROM jrottenberg/ffmpeg:5-ubuntu AS prepare_video
RUN apt-get update && apt-get install -y wget
WORKDIR /app
ENV SRC "jellyfish.mkv"
COPY ./MAppLE/dash/source.url /app/source.url
RUN wget $(cat /app/source.url) -O $SRC

COPY ./MAppLE/dash/prepare_video.sh /app/prepare_video.sh
RUN /app/prepare_video.sh 1

FROM ubuntu:bionic
USER root
RUN rm -rf /usr/local/bin/mn /usr/local/bin/mnexec \
    /usr/local/lib/python*/*/*mininet* \
    /usr/local/bin/ovs-* /usr/local/sbin/ovs-* \
    && apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y \
       mininet \
       gcc \
       python-minimal \
       python-pip \
       iputils-ping \
       iproute2 \
       curl \
       net-tools \
       python-setuptools \
       locales \
       libhdf5-dev \
       libhdf5-serial-dev \
       python-pandas \
       python-matplotlib \
       vim \
       bc \
       psmisc \
       tcpdump \
    && update-rc.d openvswitch-switch defaults
COPY docker/requirements.txt requirements.txt
RUN pip install --upgrade pip setuptools && pip install -r requirements.txt
RUN apt-get install software-properties-common -y
RUN curl -fsSL https://download.docker.com/linux/ubuntu/gpg | apt-key add -
RUN add-apt-repository \
   "deb [arch=amd64] https://download.docker.com/linux/ubuntu \
   $(lsb_release -cs) \
   stable"
RUN apt-get install docker-ce docker-ce-cli -y
WORKDIR /var/www
COPY docker/www/* /var/www
# Set the locale
RUN sed -i -e 's/# en_US ISO-8859-1/en_US ISO-8859-1/' /etc/locale.gen && \
    locale-gen
ENV LANG en_US
ENV LANGUAGE en_US:en
ENV LC_ALL en_US

# Default setting of peekaboo
# WORKDIR /notebooks
# COPY notebooks/*.ipynb /notebooks/
# WORKDIR /App
# COPY . /App
# WORKDIR /notebooks
RUN mkdir -p /docker/notebooks
WORKDIR /docker/notebooks

RUN jupyter notebook --generate-config
RUN echo '\
from IPython.lib import passwd \n\
password = passwd("mpquic") \n\
c.NotebookApp.password = password' >> /root/.jupyter/jupyter_notebook_config.py


COPY --from=builder /app/MAppLE/caddy/caddy/caddy_custom /build/caddy_custom
COPY --from=builder /app/MAppLE/proxy_module/proxy_module.so /build
RUN chmod +x /build/caddy_custom

COPY --from=prepare_video /videos /videos

COPY ./MAppLE/dash /dash
COPY ./MAppLE/astream /astream
WORKDIR /docker/mininettest
CMD service docker start && service openvswitch-switch start && jupyter notebook --ip 0.0.0.0 --allow-root
