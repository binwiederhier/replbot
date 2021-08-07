FROM docker
MAINTAINER Philipp C. Heckel <philipp.heckel@gmail.com>
RUN \
	   apk add tmux \
	&& mkdir -p /etc/replbot
COPY replbot /usr/bin

ENTRYPOINT ["replbot"]
