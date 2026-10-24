# Android SDK + Gradle build image for the Cloud Companion SMS app (kept as a cache: docker build -t phe-android-build android/docker).
FROM eclipse-temurin:17-jdk-jammy
ARG CMDLINE_TOOLS=11076708
ARG GRADLE=8.7
ENV ANDROID_HOME=/opt/android-sdk ANDROID_SDK_ROOT=/opt/android-sdk GRADLE_USER_HOME=/opt/gradle-home
RUN apt-get update && apt-get install -y --no-install-recommends unzip curl ca-certificates && rm -rf /var/lib/apt/lists/*
RUN mkdir -p $ANDROID_HOME/cmdline-tools && cd /tmp \
 && curl -fsSL -o ct.zip https://dl.google.com/android/repository/commandlinetools-linux-${CMDLINE_TOOLS}_latest.zip \
 && unzip -q ct.zip -d $ANDROID_HOME/cmdline-tools && mv $ANDROID_HOME/cmdline-tools/cmdline-tools $ANDROID_HOME/cmdline-tools/latest && rm ct.zip
RUN yes | $ANDROID_HOME/cmdline-tools/latest/bin/sdkmanager --licenses >/dev/null \
 && $ANDROID_HOME/cmdline-tools/latest/bin/sdkmanager "platforms;android-34" "build-tools;34.0.0" "platform-tools"
RUN cd /opt && curl -fsSL -o g.zip https://services.gradle.org/distributions/gradle-${GRADLE}-bin.zip && unzip -q g.zip && rm g.zip
ENV PATH=/opt/gradle-${GRADLE}/bin:$ANDROID_HOME/platform-tools:$PATH
WORKDIR /work
