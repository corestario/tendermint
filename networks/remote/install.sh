#!/usr/bin/env bash


echo "export DO_API_TOKEN=\"71a4a2b5c95e85fbae11a2afc37d61aa639d120ac284396ca8b098a93ff22b92\"" >> ~/.profile
echo "export SSH_KEY_FILE=\"\$HOME/.ssh/id_rsa.pub\"" >> ~/.profile
NUM_NODES=15
TESTNET_NAME="benchnet"

# NOTE: you must set this manually now
source ~/.profile

rm -rf $GOPATH/src/github.com/tendermint/tendermint/networks/remote/nodes/list/*
go run $GOPATH/src/github.com/tendermint/tendermint/networks/remote/nodes/node.go -N="$NUM_NODES"


cd $GOPATH/src/github.com/tendermint/tendermint/networks/remote/terraform
terraform init
terraform apply -var DO_API_TOKEN="$DO_API_TOKEN" -var SSH_KEY_FILE="$SSH_KEY_FILE" -var TESTNET_NAME="$TESTNET_NAME" -var SERVERS="$NUM_NODES" -auto-approve

# let the droplets boot
sleep 60


# all the ansible commands are also directory specific
cd $GOPATH/src/github.com/tendermint/tendermint/networks/remote/ansible

ansible-playbook -i inventory/digital_ocean.py -l $TESTNET_NAME install.yml
ansible-playbook -i inventory/digital_ocean.py -l $TESTNET_NAME config.yml -e BINARY=$GOPATH/src/github.com/tendermint/tendermint/build/tendermint -e CONFIGDIR=$GOPATH/src/github.com/tendermint/tendermint/networks/remote/nodes/list
#-e N="$NUM_NODES"

sleep 30
#Update ansble. Add persistent node connections
go run $GOPATH/src/github.com/tendermint/tendermint/networks/remote/nodes/update_template.go

# now, we can re-run the install command
ansible-playbook -i inventory/digital_ocean.py -l $TESTNET_NAME install.yml

# and finally restart it all
ansible-playbook -i inventory/digital_ocean.py -l $TESTNET_NAME restart.yml

echo "congratulations, your testnet is now running :)"
