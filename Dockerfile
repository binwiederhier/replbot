FROM ubuntu
MAINTAINER Philipp C. Heckel <philipp.heckel@gmail.com>

RUN \
	   apt-get update \
	&& apt-get install -y ca-certificates screen --no-install-recommends \
	&& rm -rf /var/lib/apt/lists/*ubuntu.{org,net}* \
	&& apt-get purge -y --auto-remove \
	&& mkdir -p /etc/replbot
COPY replbot /usr/bin

ENTRYPOINT ["replbot"]
