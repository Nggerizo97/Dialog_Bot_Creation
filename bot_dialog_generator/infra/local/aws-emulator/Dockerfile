FROM public.ecr.aws/amazonlinux/amazonlinux:2023-minimal
ARG EMULATOR_ARCH=x86_64
ADD https://downloads.aws.com/aws-emulator/packages/aws-emulator/LTS/latest/aws-emulator-${EMULATOR_ARCH}.rpm /tmp/aws-emulator.rpm
RUN rpm -i /tmp/aws-emulator.rpm && rm /tmp/aws-emulator.rpm \
    && mkdir -p /home/emulator/.aws/emulator && chown -R 1000 /home/emulator
USER 1000
ENV HOME=/home/emulator
ENV EMULATOR_TELEMETRY=off
EXPOSE 9297
ENTRYPOINT ["aws-emulator", "-b", "0.0.0.0"]
CMD ["-p", "9297", "--telemetry", "off"]
