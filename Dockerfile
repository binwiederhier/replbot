FROM docker
MAINTAINER Philipp C. Heckel <philipp.heckel@gmail.com>
RUN \
	   apk add tmux asciinema \
	&& mkdir -p /etc/replbot
COPY replbot /usr/bin

ENTRYPOINT ["replbot"]
