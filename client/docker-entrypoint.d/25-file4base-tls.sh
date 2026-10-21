#!/bin/sh
# Enable the HTTPS listener of the File4Base Web Client, but only when the
# certificate the API persists is already readable. A missing certificate
# (a first start, or a stack brought up without the shared TLS volume) leaves
# Nginx serving HTTP instead of failing to start.
set -e

cert="${FILE4BASE_TLS_CERT:-/etc/file4base/tls/server.crt}"
key="${FILE4BASE_TLS_KEY:-/etc/file4base/tls/server.key}"
conf_dir=/etc/nginx/conf.d
include_dir=/etc/nginx/file4base
tls_conf="$conf_dir/file4base-tls.conf"
force_https_conf="$include_dir/force-https.conf"

mkdir -p "$include_dir"
rm -f "$tls_conf"
: > "$force_https_conf"

if [ ! -s "$cert" ] || [ ! -s "$key" ]; then
    echo "25-file4base-tls.sh: $cert is not readable yet; serving HTTP only."
    echo "25-file4base-tls.sh: restart this container once the API has created it (docker compose restart web)."
    exit 0
fi

sed -e "s#__FILE4BASE_TLS_CERT__#$cert#" \
    -e "s#__FILE4BASE_TLS_KEY__#$key#" \
    "$include_dir/tls.conf.template" > "$tls_conf"
echo "25-file4base-tls.sh: HTTPS listener enabled with $cert"

case "${FILE4BASE_FORCE_HTTPS:-false}" in
    1 | true | TRUE | True | yes | on)
        printf 'return 301 https://$host$request_uri;\n' > "$force_https_conf"
        echo "25-file4base-tls.sh: HTTP requests are redirected to HTTPS"
        ;;
esac
