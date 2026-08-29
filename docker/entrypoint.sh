#!/bin/sh
# Generates SeaweedFS's static-identity IAM config from the operator's AWS
# credentials and starts the single-process S3 gateway. kinoview and any LAN
# checkout client authenticate with the same pair via SigV4 (see SEAWEEDFS.md).
#
# -s3.iam=false disables the embedded IAM *API* (CreateUser/ListUsers/...).
# kinoview does not use it; it also avoids the "no signing key found for STS
# service" error that the embedded IAM manager logs when no STS signing key is
# configured. Static SigV4 auth from -s3.config is unaffected by that flag.
set -e

if [ -z "${AWS_ACCESS_KEY_ID:-}" ] || [ -z "${AWS_SECRET_ACCESS_KEY:-}" ]; then
	echo "error: AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY must be set (see .env.example)" >&2
	exit 1
fi

mkdir -p /etc/seaweedfs
cat > /etc/seaweedfs/iam.json <<EOF
{"identities":[{"name":"kinoview","credentials":[{"accessKey":"${AWS_ACCESS_KEY_ID}","secretKey":"${AWS_SECRET_ACCESS_KEY}"}],"actions":["Admin","Read","Write","List","Tagging"]}]}
EOF

exec weed server \
	-s3 \
	-s3.config /etc/seaweedfs/iam.json \
	-s3.iam=false \
	-dir /data \
	-master.port=9333 \
	-volume.port=8080 \
	-filer -filer.port=8888 \
	-s3.port=8333 \
	-master.telemetry=false \
	-s3.port.iceberg=0 \
	-volume.preStopSeconds=0
