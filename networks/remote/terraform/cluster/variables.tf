variable "name" {
  description = "The cluster name, e.g cdn"
}

variable "regions" {
  description = "Regions to launch in"
  type = "list"
  default = ["NYC3"]
}

variable "ssh_key" {
  description = "SSH key filename to copy to the nodes"
  type = "string"
}

variable "instance_size" {
  description = "The instance size to use"
  default = "2gb"
}

variable "servers" {
  description = "Desired instance count"
  default     = 4
}

