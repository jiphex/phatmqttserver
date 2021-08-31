with import <nixpkgs> {};
#{ stdenv, buildGoModule, fetchFromGitHub }:
buildGoModule rec {
  name = "phatmqttserver";
  version = "0.0.10";
  src = fetchFromGitHub {
    owner = "jiphex";
    repo = name;
    rev = "v${version}";
    sha256 = "0wdn1bblr2halfklab5acj9z7aah79fkwswah0rwiv5lfwp1lg5s";
  };
  vendorSha256 = "0sa0imkm6sgq4bc1k3zml188q4z7smjphjdrkbrspdpf3i2wqyc3";
  subPackages = [ "cmd/server" ];
  runVend = false;
  meta = with stdenv.lib; {};
}
