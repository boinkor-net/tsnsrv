{
  description = "Inputs only necessary for developing tsnsrv.";
  inputs = {
    devshell.url = "github:numtide/devshell";
    generate-go-sri = {
      url = "github:antifuchs/generate-go-sri";
      inputs.nixpkgs.follows = "";
    };
    flocken = {
      url = "github:mirkolenz/flocken/v1";
      inputs.nixpkgs.follows = "";
    };
  };
  outputs = {...}: {};
}
