#!/bin/bash

register_folder=0;
while true; do
    read -p "Are you sure you want to register this folder with samba? " yn
    case $yn in
        [Yy]* ) register_folder=1; break;;
        [Nn]* ) register_folder=0; break;;
        * ) echo "Please answer yes or no.";;
    esac
done

# Read user name to set password
echo "Enter the linux userid: "
read name

# Install samba
sudo apt-get -y update
sudo apt-get -y install samba

sudo smbpasswd -a $name
if [ $? -ne 0 ]; then { echo "Failed, aborting." ; exit 1; } fi

# Register the shared folder in samba.
# Since root privileges are required, I/O is copied through tee.
if [ $register_folder -eq 1 ]; then

	cd ../
	{
		echo '[mpquic]'
		echo 'path = '${PWD}
		echo 'writeable = yes'
		echo 'create mode = 0777'
		echo 'directory mode = 0777'
	} | sudo tee -a /etc/samba/smb.conf
fi

sudo service smbd restart
