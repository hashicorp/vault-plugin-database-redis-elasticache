# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

PLUGIN_DIR=$1
PLUGIN_NAME=$2

TEST_ELASTICACHE_URL=$3
TEST_ELASTICACHE_REGION=$4
TEST_ELASTICACHE_ACCESS_KEY_ID=$5
TEST_ELASTICACHE_SECRET_ACCESS_KEY=$6

vault plugin deregister "$PLUGIN_NAME"
vault secrets disable database
killall "$PLUGIN_NAME"

rm "$PLUGIN_DIR"/"$PLUGIN_NAME"
cp ./bin/"$PLUGIN_NAME" "$PLUGIN_DIR"/"$PLUGIN_NAME"

vault secrets enable database
vault plugin register \
      -sha256="$(shasum -a 256 "$PLUGIN_DIR"/"$PLUGIN_NAME" | awk '{print $1}')" \
      database "$PLUGIN_NAME"

vault write database/config/local-redis plugin_name="$PLUGIN_NAME" \
    	allowed_roles="*" \
    	url="$TEST_ELASTICACHE_URL" \
    	region="$TEST_ELASTICACHE_REGION" \
    	access_key_id="$TEST_ELASTICACHE_ACCESS_KEY_ID" \
    	secret_access_key="$TEST_ELASTICACHE_SECRET_ACCESS_KEY"