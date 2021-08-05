FROM ubuntu
MAINTAINER Philipp C. Heckel <philipp.heckel@gmail.com>

RUN mkdir -p /etc/replbot
COPY replbot /usr/bin

ENTRYPOINT ["replbot"]
