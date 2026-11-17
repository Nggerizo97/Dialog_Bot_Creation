# AWS Emulator, for local Cognito (docs/cognito-setup.md). From the AWS Emulator User
# Guide, "Getting started with Docker": AWS publishes no prebuilt image; this installs
# the Linux package. On Apple silicon or other Arm machines, build with
#   docker build --build-arg ARCH=aarch64 -t aws-emulator infra/local/aws-emulator
FROM public.ecr.aws/amazonlinux/amazonlinux:2023-minimal
ARG ARCH=x86_64
ADD https://downloads.aws.com/aws-emulator/packages/aws-emulator/LTS/latest/aws-emulator-${ARCH}.rpm /tmp/aws-emulator.rpm
RUN rpm -i /tmp/aws-emulator.rpm && rm /tmp/aws-emulator.rpm \
    && mkdir -p /home/emulator && chown 1000 /home/emulator
USER 1000
ENV HOME=/home/emulator
# No usage telemetry from development machines (see docs/legal/THIRD_PARTY_INVENTORY.md).
ENV EMULATOR_TELEMETRY=off
EXPOSE 9297
# Listen on all addresses inside the container; compose publishes the port on 127.0.0.1 only.
ENTRYPOINT ["aws-emulator", "-b", "0.0.0.0", "--telemetry", "off"]
