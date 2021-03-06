FROM quay.io/redsift/baseos
MAINTAINER Rahul Powar email: rahul@redsift.io version: 1.1.102

RUN export DEBIAN_FRONTEND=noninteractive && \
    apt-get update && \
    apt-get install -y lsb-release unzip openssl ca-certificates curl rsync gettext-base software-properties-common python-software-properties \
    	iputils-ping dnsutils build-essential libtool autoconf git dialog man python-pip \
    	libwebkit2gtk-4.0-dev libmagickwand-dev xvfb x11-utils && \
    	pip install dockerize && \
    apt-get clean && rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*

# Go ENV vars
ENV GOPATH=/opt/gopath PATH=$PATH:/usr/local/go/bin

# Add the webp mime type as it seems to be missing
RUN echo -e "\nimage/webp webp" >> /etc/mime.types

# Cleanup default cron tasks
RUN rm -f /etc/cron.hourly/* /etc/cron.daily/* /etc/cron.weekly/*  /etc/cron.monthly/*

# Fix for ubuntu to ensure /etc/default/locale is present
RUN update-locale

# Change the onetime and fixup stage to terminate on error
# Xvfb display number set to 1
# Prevent libGL errors with indirect mode http://unix.stackexchange.com/questions/1437/what-does-libgl-always-indirect-1-actually-do
ENV DISPLAY=:1 LIBGL_ALWAYS_INDIRECT=1

# S6 default entry point is the init added from the overlay
ENTRYPOINT [ "/init" ]

WORKDIR /opt/gopath/

# Copy S6 & App
COPY root /
