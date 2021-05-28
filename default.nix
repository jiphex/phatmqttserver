with import <nixpkgs> {};
#{ stdenv, buildGoModule, fetchFromGitHub }:
buildGoModule rec {
  name = "phatmqttserver";
  version = "0.0.1";
  src = fetchFromGitHub {
    owner = "jiphex";
    repo = name;
    rev = "v${version}";
    sha256 = "1k021j79dgnhm99nsh7mhjsd75iagrl8l71cf40dwzywj119gdn7";
  };
  vendorSha256 = "1vh76157824vbvbp3j6b4zy6m9vwmjyc5hwpm2a182dy7j9y0q0x";
  subPackages = [ "cmd/imgtool" "cmd/phatmqttserver" ];
  runVend = false;
  meta = with stdenv.lib; {};
}