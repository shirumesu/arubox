package ocr

import (
	"os"
	"path/filepath"
	"runtime"
)

// asset is one file the PP-OCRv6 engine needs, and where it is downloaded
// from. The same bytes are on every source, so one checksum covers all.
type asset struct {
	// name is the file's name in the engine's folder.
	name    string
	label   string
	license string
	// size is the file's size once placed.
	size int64
	// download is what is fetched: the file itself, or a zip archive that
	// holds it as member.
	download download
	member   string
}

type download struct {
	size    int64
	sha256  string
	sources []Source
}

// Source is a host that carries a download, tried in the order listed.
type Source struct {
	Name string `json:"name"`
	url  string
}

// The models come from PaddlePaddle's own repositories, pinned to a commit:
// ModelScope is reachable from mainland China, HF-Mirror mirrors Hugging
// Face there, and Hugging Face is the original.
func modelSources(repo, modelScopeCommit, hfCommit, file string) []Source {
	return []Source{
		{Name: "ModelScope", url: "https://www.modelscope.cn/models/PaddlePaddle/" + repo + "/resolve/" + modelScopeCommit + "/" + file},
		{Name: "HF-Mirror", url: "https://hf-mirror.com/PaddlePaddle/" + repo + "/resolve/" + hfCommit + "/" + file},
		{Name: "Hugging Face", url: "https://huggingface.co/PaddlePaddle/" + repo + "/resolve/" + hfCommit + "/" + file},
	}
}

const (
	detRepo = "PP-OCRv6_small_det_onnx"
	recRepo = "PP-OCRv6_small_rec_onnx"
	// Commits of the repositories on each host.
	detModelScope = "37b02eded8dbca659f8ee5d51f822ea1ebd9bcba"
	detHF         = "28fe5895c24fd108c19eb3e8479f4ab385fbfc62"
	recModelScope = "ba215b1cc49d9ed4459d161b96778e8643fe0c1f"
	recHF         = "b8f84f0b80c529de40b4fbb3544b84fa7233a513"
	modelLicense  = "Apache-2.0"
)

var modelAssets = []asset{
	{
		name: "PP-OCRv6_small_det.onnx", label: "检测模型", license: modelLicense, size: 9880512,
		download: download{size: 9880512, sha256: "d73e0058b7a8086bbd57f3d10b8bcd4ff95363f67e06e2762b5e814fe9c9410e",
			sources: modelSources(detRepo, detModelScope, detHF, "inference.onnx")},
	},
	{
		name: "PP-OCRv6_small_rec.onnx", label: "识别模型", license: modelLicense, size: 21159378,
		download: download{size: 21159378, sha256: "5435fd747c9e0efe15a96d0b378d5bd157e9492ed8fd80edf08f30d02fa24634",
			sources: modelSources(recRepo, recModelScope, recHF, "inference.onnx")},
	},
	{
		// The recognition model's configuration holds its dictionary.
		name: "PP-OCRv6_small_rec.yml", label: "识别字典", license: modelLicense, size: 150579,
		download: download{size: 150579, sha256: "ab078671bb49f06228eadccd34f1bb501e157f7a047095ffb943ba81512c77d1",
			sources: modelSources(recRepo, recModelScope, recHF, "inference.yml")},
	},
}

// ONNX Runtime comes out of Microsoft's Python wheel, a zip archive: it
// holds the same library as the release on GitHub at under half the
// download, and PyPI is mirrored in mainland China, where GitHub is slow.
// The library inside does not depend on Python.
func wheelSources(path string) []Source {
	return []Source{
		{Name: "阿里云镜像", url: "https://mirrors.aliyun.com/pypi/packages/" + path},
		{Name: "清华大学镜像", url: "https://pypi.tuna.tsinghua.edu.cn/packages/" + path},
		{Name: "PyPI", url: "https://files.pythonhosted.org/packages/" + path},
	}
}

const runtimeLicense = "MIT"

// runtimeAssets holds the library for each system and processor it is
// built for. There is no build for Intel Macs.
var runtimeAssets = map[string]asset{
	"darwin/arm64": {
		name: "libonnxruntime.1.31.1.dylib", label: "ONNX Runtime 1.31.1", license: runtimeLicense, size: 33037336,
		download: download{size: 20882125, sha256: "96dd2564b10ea7153ad9c207e7cd8e5917cb3ba521e0e0a62ac03486fdc8d56c",
			sources: wheelSources("0e/f7/cecc0efeeea83bff2f7789ae1b80f27fa0f0210d6d9d66d1f0031a6b5e47/onnxruntime-1.31.1-cp312-cp312-macosx_14_0_arm64.whl")},
		member: "onnxruntime/capi/libonnxruntime.1.31.1.dylib",
	},
	"windows/amd64": {
		name: "onnxruntime.dll", label: "ONNX Runtime 1.31.1", license: runtimeLicense, size: 19288376,
		download: download{size: 14888466, sha256: "ae8a9a351efda685cc59a66226cdefaa37a126c1d66872ca23a2de9425326130",
			sources: wheelSources("45/b2/793b4051d8dc6acb379ead9b72050f0ffa283ca49a6904add2e218b83e2e/onnxruntime-1.31.1-cp312-cp312-win_amd64.whl")},
		member: "onnxruntime/capi/onnxruntime.dll",
	},
	"windows/arm64": {
		name: "onnxruntime.dll", label: "ONNX Runtime 1.31.1", license: runtimeLicense, size: 19171640,
		download: download{size: 14740724, sha256: "fe8b9ac1ae8e894e12c47ee82e4f6dfc1835768133687257866469fda80d0bf1",
			sources: wheelSources("ed/72/09d635ab4b6137d8ac63d4911a37dad8b1ef15a77d20b65d7ed40fdca012/onnxruntime-1.31.1-cp312-cp312-win_arm64.whl")},
		member: "onnxruntime/capi/onnxruntime.dll",
	},
}

// assets lists every file the engine needs on this computer, or nil where
// ONNX Runtime has no build.
func assets() []asset {
	rt, ok := runtimeAssets[runtime.GOOS+"/"+runtime.GOARCH]
	if !ok {
		return nil
	}
	return append([]asset{rt}, modelAssets...)
}

// installedFiles are the paths of the placed files.
type installedFiles struct {
	library, detection, recognition, dictionary string
}

// filesIn names the placed files of list, which is in the order assets
// returns: the library, then the models in the order of modelAssets.
func filesIn(dir string, list []asset) installedFiles {
	return installedFiles{
		library:     filepath.Join(dir, list[0].name),
		detection:   filepath.Join(dir, list[1].name),
		recognition: filepath.Join(dir, list[2].name),
		dictionary:  filepath.Join(dir, list[3].name),
	}
}

// installed reports whether every file is in dir at its size. Checksums
// are verified when a file is downloaded; a file damaged later fails to
// load with an error that says so.
func installed(dir string, list []asset) bool {
	if list == nil {
		return false
	}
	for _, a := range list {
		info, err := os.Stat(filepath.Join(dir, a.name))
		if err != nil || info.Size() != a.size {
			return false
		}
	}
	return true
}
