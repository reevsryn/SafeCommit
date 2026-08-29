package resolve

// distAliases maps a Python IMPORT name to the PyPI DISTRIBUTION name that
// provides it, for cases where the two differ by actual letters.
//
// # Why this table is a precision-critical component
//
// Import names and distribution names are different namespaces. `import yaml`
// is provided by a project called PyYAML; there is no PyPI project named
// `yaml`. A detector that looks up import names directly against the registry
// therefore reports 404 — "hallucinated!" — for some of the most widely used
// packages in Python. Every missing entry here is a false positive waiting to
// happen against real, correct code.
//
// PEP 503 normalization already handles the easy mismatches (case, and -_.
// interchange), so `Jinja2`/`jinja2` and `flask_cors`/`Flask-Cors` need no
// entry. Only letter-level differences belong here.
//
// # Known limitation
//
// This table is finite and the real mapping is not. PyPI exposes no
// import-name -> distribution-name index, so there is no complete oracle to
// consult. Coverage here is "the mismatches common enough to appear in real
// diffs", which is a judgement call, not a guarantee. The confidence gate
// (step 5) is where an unknown-but-plausible name should be held back rather
// than fired on; until then, treat residual false positives on this path as
// expected and measure them.
var distAliases = map[string]string{
	// Ported from bench/verify.py, which used them for corpus QA.
	"yaml":          "PyYAML",
	"PIL":           "Pillow",
	"cv2":           "opencv-python",
	"bs4":           "beautifulsoup4",
	"dateutil":      "python-dateutil",
	"sklearn":       "scikit-learn",
	"attr":          "attrs",
	"jwt":           "PyJWT",
	"dotenv":        "python-dotenv",
	"OpenSSL":       "pyOpenSSL",
	"Crypto":        "pycryptodome",
	"git":           "GitPython",
	"pkg_resources": "setuptools",
	"_pytest":       "pytest",

	// Additional common mismatches.
	"serial":       "pyserial",
	"usb":          "pyusb",
	"Xlib":         "python-xlib",
	"docx":         "python-docx",
	"pptx":         "python-pptx",
	"fitz":         "PyMuPDF",
	"magic":        "python-magic",
	"MySQLdb":      "mysqlclient",
	"skimage":      "scikit-image",
	"nacl":         "PyNaCl",
	"Levenshtein":  "python-Levenshtein",
	"slugify":      "python-slugify",
	"mpl_toolkits": "matplotlib",
	"gi":           "PyGObject",
	"cairo":        "pycairo",
	"OpenGL":       "PyOpenGL",
	"zmq":          "pyzmq",
	"psutil":       "psutil",
	"dns":          "dnspython",
	"jose":         "python-jose",
	"multipart":    "python-multipart",
	"snappy":       "python-snappy",
	"memcache":     "python-memcached",
	"ldap":         "python-ldap",
	"OpenSSL_":     "pyOpenSSL",
	"win32api":     "pywin32",
	"win32com":     "pywin32",
	"win32con":     "pywin32",
	"pythoncom":    "pywin32",
	"pywintypes":   "pywin32",
	"pkg_config":   "pkgconfig",
	"ruamel":       "ruamel.yaml",
	"google":       "protobuf",
	"grpc":         "grpcio",
	"grpc_tools":   "grpcio-tools",
	"jinja2":       "Jinja2",
	"markdown":     "Markdown",
	"yattag":       "yattag",
	"tqdm":         "tqdm",
	"regex":        "regex",
	"sqlalchemy":   "SQLAlchemy",
}

// DistFor returns the distribution name to look up for an import name, and
// whether an alias was applied.
func DistFor(importName string) (string, bool) {
	if d, ok := distAliases[importName]; ok {
		return d, true
	}
	return importName, false
}
