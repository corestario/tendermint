# Remote Cluster with Terraform and Ansible

See the [docs](https://tendermint.com/docs/networks/terraform-and-ansible.html).

Requirements:
1) Install:
1.1) Terraform
```
wget https://releases.hashicorp.com/terraform/0.11.7/terraform_0.11.7_linux_amd64.zip
unzip terraform_0.11.7_linux_amd64.zip -d /usr/bin/
```
1.2) Ansible
``` sudo apt-add-repository ppa:ansible/ansible -y
sudo apt-get update -y
sudo apt-get install ansible -y
pip install dopy
```
2) Environment variables
2.1) Setup your DO API token
`echo "export DO_API_TOKEN=\"yourToken\"" >> ~/.profile`
2.2) Setup your ssh key
`echo "export SSH_KEY_FILE=\"\$HOME/.ssh/id_rsa.pub\"" >> ~/.profile`
3) Build your tendermint application(it should be there `$GOPATH/src/github.com/tendermint/tendermint/build/tendermint)`

Deploy cluster:
```
terraform init
./install.sh
```
