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
// diffs", which is a judgement call, not a guarantee.
//
// Measured coverage: holdout #1 drove 203 registry resolutions across ten
// unfamiliar dependency surfaces with zero alias failures; holdout #2 produced
// exactly one gap (`opentelemetry`, since added). So the table is thin but has
// held up at roughly one miss per 280 real PRs. A confidence gate that held
// back unknown-but-plausible names remains deferred (PHASE1-NOTES.md R5) --
// deliberately, because it would trade recall on novel hallucinations against
// a failure rate this low.
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
	"dns":          "dnspython",
	"jose":         "python-jose",
	"multipart":    "python-multipart",
	"snappy":       "python-snappy",
	"memcache":     "python-memcached",
	"ldap":         "python-ldap",
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

	// --- Namespace-package roots ---------------------------------------
	//
	// A PEP 420 namespace root is an import name that no single distribution
	// owns: many packages contribute subpackages beneath it, and the root name
	// itself is frequently not registered at all. A direct lookup therefore
	// 404s on a real, widely used library. Mapping to any distribution that
	// populates the namespace suffices, because the only question we put to the
	// registry is "does something by this name exist".
	//
	// Provenance, because it changes what the numbers mean:
	//   * `opentelemetry` was found by holdout #2, where it produced 2 of the 3
	//     false positives (PHASE1-NOTES.md R7). Adding it is a TUNED change and
	//     burns that corpus.
	//   * the other three were derived from the same pattern independently and
	//     each verified against PyPI (import name 404, mapped distribution 200).
	//     They are not corpus-derived.
	//
	// Import names that ARE themselves registered distributions were checked
	// and deliberately omitted -- azure, zope, paste, backports,
	// mypy_extensions all resolve without help. See
	// TestSelfRegisteredNamesHaveNoAlias.
	"opentelemetry": "opentelemetry-api",
	"repoze":        "repoze.lru",
	"sphinxcontrib": "sphinxcontrib-applehelp",
	"jaraco":        "jaraco.classes",
}

// DistFor returns the distribution name to look up for an import name, and
// whether an alias was applied.
func DistFor(importName string) (string, bool) {
	if d, ok := distAliases[importName]; ok {
		return d, true
	}
	return importName, false
}
