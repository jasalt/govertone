# -*- mode: ruby -*-
# vi: set ft=ruby :
#

# Vagrantfile for launching graphical Fedora 44 VM environment useful as a
# development sandbox. Mounts the repository (cwd) at /home/vagrant/govertone

# Based on (outdated) Vagrantfile at https://fedoraproject.org/wiki/Vagrant

# Usage on Mac or Windows requires setting up compatible VM provider.
# See also VirtualBox image variant available at:
# https://mirrors.nxthost.com/fedora/releases/44/Cloud/x86_64/images/

# TODO
# - include all development environment dependencies
# - setup audio for testing
# - figure out how to avoid SELinux warnings with the mounted directory

 VAGRANTFILE_API_VERSION = "2"

 Vagrant.configure(VAGRANTFILE_API_VERSION) do |config|
  # If you'd prefer to pull your boxes from Hashicorp's repository, you can
  # replace the config.vm.box and config.vm.box_url declarations with the line below.
  #
  # config.vm.box = "fedora/44-cloud-base"
  config.vm.box = "f44-cloud-libvirt"
  config.vm.box_url = "https://mirrors.nxthost.com/fedora/releases/44/Cloud/x86_64/images/Fedora-Cloud-Base-Vagrant-libvirt-44-1.7.x86_64.vagrant.libvirt.box"

  config.ssh.forward_x11 = true

  # This is an optional plugin that, if installed, updates the host's /etc/hosts
  # file with the hostname of the guest VM. In Fedora it is packaged as
  # vagrant-hostmanager
  if Vagrant.has_plugin?("vagrant-hostmanager")
      config.hostmanager.enabled = true
      config.hostmanager.manage_host = true
  end

  # Vagrant can share the source directory using rsync, NFS, or SSHFS (with the vagrant-sshfs
  # plugin). Consult the Vagrant documentation if you do not want to use SSHFS.
  config.vm.synced_folder ".", "/vagrant", disabled: true
  # config.vm.synced_folder ".", "/home/vagrant/devel", type: "sshfs", sshfs_opts_append: "-o nonempty"

  # To cache update packages (which is helpful if frequently doing vagrant destroy && vagrant up)
  # you can create a local directory and share it to the guest's DNF cache. Uncomment the lines below
  # to create and use a dnf cache directory
  #
  # Dir.mkdir('.dnf-cache') unless File.exists?('.dnf-cache')
  # config.vm.synced_folder ".dnf-cache", "/var/cache/dnf", type: "sshfs", sshfs_opts_append: "-o nonempty"

  # Comment this line if you would like to disable the automatic update during provisioning
  config.vm.provision "shell", inline: "sudo dnf upgrade -y"

  config.vm.provision "shell", inline: "sudo dnf -y install python3-dnf python3-libselinux npm ripgrep tmux make"

  # Bootstrap the govertone project
  config.vm.provision "shell", inline: "cd /home/vagrant/govertone && make bootstrap"

  # config.vm.provision "shell", inline: "sudo npm install -g agent-browser"
  config.vm.provision "shell", inline: "npm install -g --ignore-scripts @earendil-works/pi-coding-agent"

  # https://github.com/cli/cli/blob/trunk/docs/install_linux.md#dnf4
  # sudo dnf install 'dnf-command(config-manager)'
  # sudo dnf config-manager --add-repo https://cli.github.com/packages/rpm/gh-cli.repo
  # sudo dnf install gh

  # Set same UID / GID as host user so shared folder can be accessed without
  # permission changes
  host_uid = `id -u`.strip
  host_gid = `id -g`.strip

  # Setup X11 desktop with OpenBox

  config.vm.provision "shell", inline: <<-SHELL
  sudo dnf install -y xorg-x11-server-Xorg xorg-x11-xinit \
    xorg-x11-drv-qxl openbox obconf xdg-utils xterm

  # Autologin vagrant on tty1 and start X for that session.
  echo 'vagrant:vagrant' | sudo chpasswd
  sudo mkdir -p /etc/systemd/system/getty@tty1.service.d
  cat <<'EOF' | sudo tee /etc/systemd/system/getty@tty1.service.d/autologin.conf >/dev/null
[Service]
ExecStart=
ExecStart=-/sbin/agetty --autologin vagrant --noclear %I $TERM
EOF

  cat <<'EOF' | sudo tee /etc/profile.d/vagrant-startx.sh >/dev/null
if [ "$(id -un)" = vagrant ] && [ "$(tty)" = /dev/tty1 ] && [ -z "$DISPLAY" ]; then
  exec startx
fi
EOF

  cat <<'EOF' | sudo tee /home/vagrant/.xinitrc >/dev/null
xterm -e bash -c '
  cd /home/vagrant/govertone
  printf "\\nBuild and run the README demo:\\n  make build && ./out/lgs repl\\n  music.core=> (play :sine :a4 {:dur 1})\\n\\n"
  exec bash -i
' &
exec openbox-session
EOF
  sudo chown vagrant:vagrant /home/vagrant/.xinitrc
  sudo systemctl daemon-reload
  sudo systemctl enable getty@tty1
  sudo systemctl restart getty@tty1
SHELL

  # Create the "nuancier" box
  config.vm.define "govertone" do |govertone|
    govertone.vm.host_name = "govertone-dev.example.com"
 
    govertone.vm.provider :libvirt do |domain, override|
       domain.cpus = 4
       domain.graphics_type = "spice"
       domain.memory = 4096
       domain.video_type = "qxl"
       # Send the guest sound device to the connected SPICE virt-viewer.
       domain.sound_type = "ich6"

       # Required for virtiofs shared folders
       domain.qemu_use_session = false          # use qemu:///system
       domain.memorybacking :access, mode: "shared"

       # Uncomment the following line if you would like to enable libvirt's unsafe cache
       # mode. It is called unsafe for a reason, as it causes the virtual host to ignore all
       # fsync() calls from the guest. Only do this if you are comfortable with the possibility of
       # your development guest becoming corrupted (in which case you should only need to do a
       # vagrant destroy and vagrant up to get a new one).
       #
       # domain.volume_cache = "unsafe"

       # Use virtiofs for two-way host<->guest file sharing
       override.vm.synced_folder ".", "/home/vagrant/govertone", type: "virtiofs"
     end
  end
 end
