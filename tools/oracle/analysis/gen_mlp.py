import re, sys
src = open(sys.argv[1]).read()
out = ["// Code generated from libopus 1.6.1 src/mlp_data.c. DO NOT EDIT.",
       "",
       "// Ported from libopus 1.6.1 src/mlp_data.c.",
       "",
       "package opus",
       ""]
names = {"layer0_weights": "layer0Weights", "layer0_bias": "layer0Bias",
         "layer1_weights": "layer1Weights", "layer1_recur_weights": "layer1RecurWeights",
         "layer1_bias": "layer1Bias", "layer2_weights": "layer2Weights", "layer2_bias": "layer2Bias"}
for m in re.finditer(r"static const opus_int8 (\w+)\[(\d+)\] = \{(.*?)\};", src, re.S):
    name, n, body = m.group(1), int(m.group(2)), m.group(3)
    vals = [int(v) for v in body.replace("\n", " ").split(",") if v.strip()]
    assert len(vals) == n, (name, len(vals), n)
    out.append(f"// {names[name]} is {name}.")
    out.append(f"var {names[name]} = [{n}]int8{{")
    for i in range(0, n, 8):
        out.append("\t" + ", ".join(str(v) for v in vals[i:i+8]) + ",")
    out.append("}")
    out.append("")
out.append("""// layer0 is layer0.
var layer0 = analysisDenseLayer{
	bias:         layer0Bias[:],
	inputWeights: layer0Weights[:],
	nbInputs:     25,
	nbNeurons:    32,
	sigmoid:      false,
}

// layer1 is layer1.
var layer1 = analysisGRULayer{
	bias:             layer1Bias[:],
	inputWeights:     layer1Weights[:],
	recurrentWeights: layer1RecurWeights[:],
	nbInputs:         32,
	nbNeurons:        24,
}

// layer2 is layer2.
var layer2 = analysisDenseLayer{
	bias:         layer2Bias[:],
	inputWeights: layer2Weights[:],
	nbInputs:     24,
	nbNeurons:    2,
	sigmoid:      true,
}""")
open(sys.argv[2], "w").write("\n".join(out) + "\n")
