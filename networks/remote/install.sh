#!/usr/bin/env bash

# NOTE: you must set this manually now
source ~/.profile


cd $GOPATH/src/github.com/tendermint/tendermint/networks/remote/terraform
terraform init
terraform apply -var DO_API_TOKEN="$DO_API_TOKEN" -var SSH_KEY_FILE="$SSH_KEY_FILE" -var TESTNET_NAME="testnet" -auto-approve

# let the droplets boot
sleep 60

# get the IPs
ip0=`terraform output -json public_ips | jq '.value[0]'`
ip1=`terraform output -json public_ips | jq '.value[1]'`
ip2=`terraform output -json public_ips | jq '.value[2]'`
ip3=`terraform output -json public_ips | jq '.value[3]'`

# to remove quotes
strip() {
  opt=$1
  temp="${opt%\"}"
  temp="${temp#\"}"
  echo $temp
}

ip0=$(strip $ip0)
ip1=$(strip $ip1)
ip2=$(strip $ip2)
ip3=$(strip $ip3)

# all the ansible commands are also directory specific
cd $GOPATH/src/github.com/tendermint/tendermint/networks/remote/ansible

ansible-playbook -i inventory/digital_ocean.py -l testnet install.yml
ansible-playbook -i inventory/digital_ocean.py -l testnet config.yml -e BINARY=$GOPATH/src/github.com/tendermint/tendermint/build/tendermint -e CONFIGDIR=$GOPATH/src/github.com/tendermint/tendermint/docs/examples

sleep 10

# get each nodes ID then populate the ansible file
id0=`curl $ip0:26657/status | jq .result.node_info.id`
id1=`curl $ip1:26657/status | jq .result.node_info.id`
id2=`curl $ip2:26657/status | jq .result.node_info.id`
id3=`curl $ip3:26657/status | jq .result.node_info.id`

id0=$(strip $id0)
id1=$(strip $id1)
id2=$(strip $id2)
id3=$(strip $id3)

# remove file we'll re-write to with new info
old_ansible_file=$GOPATH/src/github.com/tendermint/tendermint/networks/remote/ansible/roles/install/templates/systemd.service.j2
rm $old_ansible_file

# need to populate the `--p2p.persistent_peers` flag
echo "[Unit]
Description={{service}}
Requires=network-online.target
After=network-online.target

[Service]
Restart=on-failure
User={{service}}
Group={{service}}
PermissionsStartOnly=true
ExecStart=/usr/bin/tendermint node --proxy_app=kvstore --p2p.persistent_peers=$id0@$ip0:26656,$id1@$ip1:26656,$id2@$ip2:26656,$id3@$ip3:26656
ExecReload=/bin/kill -HUP \$MAINPID
KillSignal=SIGTERM

[Install]
WantedBy=multi-user.target
" >> $old_ansible_file

# now, we can re-run the install command
ansible-playbook -i inventory/digital_ocean.py -l testnet install.yml

# and finally restart it all
ansible-playbook -i inventory/digital_ocean.py -l testnet restart.yml

echo "congratulations, your testnet is now running :)"
