# ref. https://hub.docker.com/r/fluent/fluentd/
FROM fluent/fluentd:v1.12.1-debian-1.0

# ref. https://github.com/fluent/fluentd-kubernetes-daemonset/blob/ea92038/docker-image/v1.12/debian-s3/Dockerfile
USER root
WORKDIR /home/fluent
ENV PATH /fluentd/vendor/bundle/ruby/2.6.0/bin:$PATH
ENV GEM_PATH /fluentd/vendor/bundle/ruby/2.6.0
ENV GEM_HOME /fluentd/vendor/bundle/ruby/2.6.0
# skip runtime bundler installation
ENV FLUENTD_DISABLE_BUNDLER_INJECTION 1
# Use the latest jemalloc https://github.com/jemalloc/jemalloc/releases
# ref. https://github.com/fluent/fluentd-docker-image/blob/4e08108/v1.12/debian/Dockerfile#L38-L41
ENV JEMALLOC_VERSION 5.2.1

COPY Gemfile* /fluentd/
RUN buildDeps="sudo make gcc g++ libc-dev libffi-dev wget bzip2" \
	runtimeDeps="curl" \
	&& apt-get update \
	&& apt-get upgrade -y \
	&& apt-get install \
	-y --no-install-recommends \
	$buildDeps $runtimeDeps net-tools \
	&& wget -O /tmp/jemalloc-${JEMALLOC_VERSION}.tar.bz2 https://github.com/jemalloc/jemalloc/releases/download/${JEMALLOC_VERSION}/jemalloc-${JEMALLOC_VERSION}.tar.bz2 \
        && cd /tmp && tar -xjf jemalloc-${JEMALLOC_VERSION}.tar.bz2 && cd jemalloc-${JEMALLOC_VERSION}/ \
        && ./configure && make \
        && mv lib/libjemalloc.so.2 /usr/lib \
	&& gem install bundler --version 2.2.6 \
	&& bundle config silence_root_warning true \
	&& bundle config set path /fluentd/vendor/bundle \
	&& bundle install --gemfile=/fluentd/Gemfile \
	&& SUDO_FORCE_REMOVE=yes \
	apt-get purge -y --auto-remove \
	-o APT::AutoRemove::RecommendsImportant=false \
	$buildDeps \
	&& rm -rf /var/lib/apt/lists/* \
	&& gem sources --clear-all \
	&& rm -rf /tmp/* /var/tmp/* /usr/lib/ruby/gems/*/cache/*.gem

COPY ./fluent.conf /fluentd/etc/

# ref. https://github.com/fluent/fluentd-docker-image/blob/c848dfe/v1.12/debian/Dockerfile#L62-L64
# ref. https://github.com/fluent/fluentd-docker-image/blob/c848dfe/v1.12/debian/entrypoint.sh
ENTRYPOINT ["tini", "--", "fluentd", "-c", "/fluentd/etc/fluent.conf", "-p", "/fluentd/plugins", "--gemfile", "/fluentd/Gemfile"]
CMD []
