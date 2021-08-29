with import <nixpkgs> {};
#{ stdenv, buildGoModule, fetchFromGitHub }:
buildGoModule rec {
  name = "phatmqttserver";
  version = "0.0.3";
  src = fetchFromGitHub {
    owner = "jiphex";
    repo = name;
    rev = "v${version}";
    sha256 = "0ginmy1msf4h3p0qr8r9fqqssk2vkrcllg3csp96mbcw126y6pn1";
  };
  vendorSha256 = "03hz53j02xalaphm0k5dz226v9cjrkzq1n54x48sqnlmfc7ccyys";
  subPackages = [ "cmd/imgtool" "cmd/server" ];
  runVend = false;
  meta = with stdenv.lib; {};
}
