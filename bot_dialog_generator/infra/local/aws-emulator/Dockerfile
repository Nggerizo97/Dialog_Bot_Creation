# AWS Emulator, for local AWS APIs including Cognito (docs/cognito-setup.md). AWS
# publishes no prebuilt image; this installs the Linux package. On Apple silicon or
# other Arm machines, build with --build-arg ARCH=aarch64.
FROM public.ecr.aws/amazonlinux/amazonlinux:2023-minimal
ARG ARCH=x86_64
ADD https://downloads.aws.com/aws-emulator/packages/aws-emulator/LTS/latest/aws-emulator-${ARCH}.rpm /tmp/aws-emulator.rpm
RUN rpm -i /tmp/aws-emulator.rpm && rm /tmp/aws-emulator.rpm \
    && mkdir -p /home/emulator/.aws/emulator && chown -R 1000 /home/emulator
USER 1000
ENV HOME=/home/emulator
ENV EMULATOR_TELEMETRY=off
EXPOSE 9297
# Listen on all addresses inside the container; compose publishes the port on 127.0.0.1 only.
ENTRYPOINT ["aws-emulator", "-b", "0.0.0.0"]
CMD ["-p", "9297", "--telemetry", "off"]
