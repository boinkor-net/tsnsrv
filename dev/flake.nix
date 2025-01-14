{
  description = "Inputs only necessary for developing tsnsrv.";
  inputs = {
    devshell.url = "github:numtide/devshell";
    generate-go-sri.url = "github:antifuchs/generate-go-sri";
    flocken.url = "github:mirkolenz/flocken/v2";
  };
  outputs = {...}: {};
}
