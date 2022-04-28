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
    && python -m pip install --upgrade pip \
    && python -m pip install jupyter \
    && python -m pip install matplotlib=="2.0.2" \
    && python -m pip install keras-rl \
    && python -m pip install --upgrade --user tensorflow==1.13.1 \
    && update-rc.d openvswitch-switch defaults
RUN apt-get install software-properties-common -y
RUN curl -fsSL https://download.docker.com/linux/ubuntu/gpg | apt-key add -
RUN add-apt-repository \
   "deb [arch=amd64] https://download.docker.com/linux/ubuntu \
   $(lsb_release -cs) \
   stable"
RUN apt-get install docker-ce docker-ce-cli -y
WORKDIR /var/www
COPY www/* /var/www
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

CMD service docker start && service openvswitch-switch start && jupyter notebook --ip 0.0.0.0 --allow-root
