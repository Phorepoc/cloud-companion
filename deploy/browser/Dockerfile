# SPDX-License-Identifier: Apache-2.0
# Firefox and Neko's server, without Neko's web client: the Browser page of the web app is the only way in.
# Only Neko's own files come from its image; everything else is the least Debian that runs them (no LLVM, FFmpeg, python, font packs).
# Pinned by the Debian release the Neko files were built on.
FROM ghcr.io/m1k1o/neko/base:latest AS neko

FROM debian:trixie-slim
# software WebGL (LLVM, Mesa, about 200 MB) stays: sites expect it. Build with PHE_BROWSER_WEBGL=0 to leave it out.
ARG PHE_BROWSER_WEBGL=1
RUN apt-get update && apt-get install -y --no-install-recommends \
      firefox-esr openbox xserver-xorg-core xserver-xorg-video-dummy xkb-data x11-xkb-utils pulseaudio \
      gstreamer1.0-plugins-base gstreamer1.0-plugins-good gstreamer1.0-x gstreamer1.0-pulseaudio gstreamer1.0-plugins-ugly \
      libgtk-3-0t64 libxtst6 libxcvt0 \
      fonts-liberation fonts-dejavu-core fonts-noto-color-emoji fonts-wqy-microhei zip ca-certificates \
 && if [ "$PHE_BROWSER_WEBGL" != 1 ]; then dpkg --purge --force-depends libllvm19 libz3-4 mesa-libgallium libgl1-mesa-dri; fi \
 && dpkg --purge --force-depends libspectre1 libgs10 libgs10-common libgs-common poppler-data fonts-urw-base35 2>/dev/null; \
 cd /usr/share && rm -rf doc/* man/* locale/[!e]* /var/lib/apt/lists/* /usr/lib/firefox-esr/crashreporter /var/cache/apt/*
COPY --from=neko /usr/bin/neko /usr/bin/neko
COPY --from=neko /usr/lib/xorg/modules/input/neko_drv.so /usr/lib/xorg/modules/input/neko_drv.so
COPY xorg.conf /etc/neko/xorg.conf
COPY --from=neko /etc/pulse/default.pa /etc/pulse/default.pa
COPY ext /opt/ext
COPY mozilla.cfg /usr/lib/firefox-esr/mozilla.cfg
COPY autoconfig.js /usr/lib/firefox-esr/defaults/pref/autoconfig.js
COPY start.sh /usr/local/bin/phe-browser-start
RUN chmod +x /usr/local/bin/phe-browser-start; cd /opt/ext && zip -qr /opt/bridge.xpi . && rm -f /usr/bin/zip \
 && useradd -m -u 1000 neko && usermod -aG audio,video neko \
 && mkdir -p /usr/share/firefox-esr/distribution /home/neko/.config/mozilla /home/neko/Downloads /tmp/.X11-unix /etc/neko && chmod 1777 /tmp/.X11-unix \
 && chown -R neko:neko /home/neko \
 && echo 'pref("xpinstall.signatures.required", false);' > /etc/firefox-esr/cc.js
ENV NEKO_SERVER_BIND=:8080 USER=neko DISPLAY=:99.0 PULSE_SERVER=unix:/tmp/pulseaudio.socket XDG_RUNTIME_DIR=/tmp/runtime-neko NEKO_PLUGINS_ENABLED=false
ENTRYPOINT ["/usr/local/bin/phe-browser-start"]
