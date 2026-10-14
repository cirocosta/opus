import re, sys
src = open(sys.argv[1]).read()
out = ["// Code generated from libopus 1.6.1 src/mapping_matrix.c by gen_tables.py. DO NOT EDIT.", "", "// Ported from libopus 1.6.1 src/mapping_matrix.c.", "", "package opus", ""]
def camel(n):
    parts = n.split('_')
    return parts[0] + ''.join(p[:1].upper()+p[1:] for p in parts[1:])
for m in re.finditer(r'const MappingMatrix (\w+) = \{ (\d+), (\d+), (\d+) \};\s*const opus_int16 (\w+)\[(\d+)\] = \{([^}]*)\};', src):
    name, r, c, g, dname, n, body = m.groups()
    vals = [v.strip() for v in body.replace('\n',' ').split(',') if v.strip()]
    assert len(vals) == int(n), (dname, len(vals), n)
    assert int(r)*int(c) == int(n)
    out.append(f"// {camel(name)} ports {name}.")
    out.append(f"var {camel(name)} = mappingMatrix{{rows: {r}, cols: {c}, gain: {g}}}")
    out.append("")
    out.append(f"// {camel(dname)} ports {dname}.")
    out.append(f"var {camel(dname)} = [{n}]int16{{")
    for i in range(0, len(vals), 8):
        out.append("\t" + ", ".join(vals[i:i+8]) + ",")
    out.append("}")
    out.append("")
print("\n".join(out).rstrip("\n"))
