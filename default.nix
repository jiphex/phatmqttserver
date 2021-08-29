with import <nixpkgs> {};
#{ stdenv, buildGoModule, fetchFromGitHub }:
buildGoModule rec {
  name = "phatmqttserver";
  version = "0.0.2";
  src = fetchFromGitHub {
    owner = "jiphex";
    repo = name;
    rev = "v${version}";
    sha256 = "0ginmy1msf4h3p0qr8r9fqqssk2vkrcllg3csp96mbcw126y6pn1";
  };
  vendorSha256 = "199lv569msazw81f0r4sybpf6vdm3ygxg3acdjm16pq72a1hdv8a";
  subPackages = [ "cmd/imgtool" "cmd/server" ];
  runVend = false;
  meta = with stdenv.lib; {};
}
