package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/redsift/go-render"
	"github.com/redsift/go-render/lightbox"
	"github.com/redsift/go-render/lightbox/constants"
	"gopkg.in/alecthomas/kingpin.v2"
	"io"
	"io/ioutil"
	"net/url"
	"os"
	"reflect"
	"strings"
	"text/template"
	"golang.org/x/net/context"
)

// LIBGL_DEBUG=verbose to debug libGl issues

// These version tags are set from the git values during CI built
// and need to be var so ldflags can change them
var (
	Tag       = ""
	Commit    = ""
	Timestamp = ""
)

const urlHelp = "URL(s) to render. If not supplied, stdin is read while open"

var (
	app             = kingpin.New("render", "Command-line WebKit based web page rendering tool.")
	debugOpt        = app.Flag("debug", "Enable debug mode.").Short('d').Default("false").Bool()
	uaAppNameOpt    = app.Flag("user-agent-app", "User agent application name.").Default("go-render").String()
	uaAppVersionOpt = app.Flag("user-agent-version", "User agent application version.").Default(Tag).String()
	consoleOpt      = app.Flag("console", "Output webpage console to stdout.").Default("false").Bool()
	timeoutOpt      = app.Flag("timeout", "Timeout for page load.").Short('t').Duration()
	languageOpt 	= app.Flag("accept-language", "Comma seperated list of Accept-Languages.").String()

	snapshotCommand     = app.Command("snapshot", "Generate a snapshot of the page.")
	snapshotFormat      = snapshotCommand.Flag("format", "File format for output").Short('f').Default("auto").Enum("auto", "png", "jpeg", "webp", "gif", "mono")
	snapshotQuality     = snapshotCommand.Flag("quality", "Quality of image when using lossy compression, values > 100 indicate lossless if available for the selected format").PlaceHolder("[0-100]").Default("75").Int()
	snapshotOutput      = snapshotCommand.Flag("output", "Filename for output").Short('o').String()
	snapshotNoImagesOpt = snapshotCommand.Flag("noimages", "Don't load images from webpage.").Bool()
	snapshotOpt         = urlsList(snapshotCommand.Arg("url", urlHelp))

	javascriptCommand   = app.Command("javascript", "Execute javascript in the context of the page.")
	javascriptContent   = javascriptCommand.Flag("js", "JavaScript file or string to execute").Short('j').Required().String()
	javascriptFormat    = javascriptCommand.Flag("format", "Format the output using the given go template").Short('f').Default("").String()
	javascriptImagesOpt = javascriptCommand.Flag("images", "Load images from webpage.").Default("false").Bool()
	javascriptOpt       = urlsList(javascriptCommand.Arg("url", urlHelp))

	metadataCommand   = app.Command("metadata", "Get page metadata.")
	metadataFormat    = metadataCommand.Flag("format", "Format the output using the given go template").Short('f').Default("").String()
	metadataImagesOpt = metadataCommand.Flag("images", "Load images from webpage.").Default("false").Bool()
	metadataOpt       = urlsList(metadataCommand.Arg("url", urlHelp))
)

type timing struct {
	Start  float64
	Load   float64
	Finish float64
}

type metadata struct {
	Title  string
	URI    string
	Timing timing
}

// Git returns a string representing the Tag and/or Commit of the codebase that built this binary
func Git() string {
	if Tag == "" {
		if Commit == "" {
			return "unknown"
		}
		return Commit
	}
	return fmt.Sprintf("%s-%s", Tag, Commit)
}

// Version returns the Git() version and build date for this binary
func Version() string {
	git := Git()
	if Timestamp == "" {
		return git
	}
	return fmt.Sprintf("%s-%s", git, Timestamp)
}


func newContext() context.Context {
	ctx := context.Background()
	if t := *timeoutOpt; t > 0 {
		ctx, _ = context.WithTimeout(ctx, t)
	}

	return ctx
}

func newLoadedView(ctx context.Context, url *url.URL, autoLoadImages bool) *render.View {
	u := url.String()

	r, err := render.NewRenderer()
	app.FatalIfError(err, "Unable to create renderer")

	v := r.NewView(*uaAppNameOpt, *uaAppVersionOpt, autoLoadImages, *consoleOpt, strings.Split(*languageOpt, ","))

	if *debugOpt {
		fmt.Printf("Loading URL:%q, Images:%t\n", u, autoLoadImages)
	}

	err = v.LoadURI(u)
	app.FatalIfError(err, "Unable to request URL %q", u)

	err = v.Wait(ctx)
	app.FatalIfError(err, "Unable to load page")

	return v
}

func formatInterface(m interface{}, tmpl string) string {
	var b []byte
	var err error

	// Based on docker template functions
	var templateFuncs = template.FuncMap{
		"json": func(m interface{}) string {
			a, _ := json.MarshalIndent(m, "", "\t")
			return string(a)
		},
		"split": strings.Split,
		"join":  strings.Join,
		"title": strings.Title,
		"lower": strings.ToLower,
		"upper": strings.ToUpper,
	}

	if tmpl != "" {
		temp, err := template.New("").Funcs(templateFuncs).Parse(tmpl)
		app.FatalIfError(err, "Unable to parse template")

		buffer := new(bytes.Buffer)
		err = temp.Execute(buffer, m)
		app.FatalIfError(err, "Unable to format data")

		b = buffer.Bytes()
	} else {
		b, err = json.MarshalIndent(m, "", "\t")
		app.FatalIfError(err, "Unable to format data")
	}
	return string(b)
}

func processURL(arg *url.URL) *url.URL {
	if arg.Scheme == "" {
		arg.Scheme = "http"
		arg, _ = url.Parse(arg.String())
	}

	return arg
}

func urls(arg []*url.URL) chan *url.URL {
	urls := make(chan *url.URL, 1)

	go func() {
		defer close(urls)
		if arg != nil && len(arg) > 0 {

			for _, u := range arg {
				urls <- processURL(u)
			}
		} else {
			// stdin
			scanner := bufio.NewScanner(os.Stdin)
			for scanner.Scan() {
				t := scanner.Text()
				if t == "" {
					continue
				}
				u, err := url.Parse(t)
				app.FatalIfError(err, "Could not parse URL")
				urls <- processURL(u)
			}
		}
	}()

	return urls
}

type filenameMetadata struct {
	Host  string
	Index int
}

func createFilename(temp *template.Template, url *url.URL, index int) string {
	m := &filenameMetadata{url.Host, index}

	buffer := new(bytes.Buffer)
	err := temp.Execute(buffer, m)
	app.FatalIfError(err, "Unable to format name")

	s := string(buffer.Bytes())

	if *debugOpt {
		fmt.Printf("Writing output as file:%s\n", s)
	}

	return s
}

func snapshot(url *url.URL, index int, optFmt constants.Format, t *template.Template) {
	ctx := newContext()

	v := newLoadedView(ctx, url, !*snapshotNoImagesOpt)
	defer v.Close()

	i, err := v.NewSnapshot(ctx)
	app.FatalIfError(err, "Unable to create snapshot")

	if i.Pix == nil {
		app.Fatalf("No Pix in captured image")
	}

	if i.Stride == 0 || i.Rect.Max.X == 0 || i.Rect.Max.Y == 0 {
		app.Fatalf("No image data in captured image")
	}

	var out io.Writer
	outFmt := optFmt

	if t == nil {
		// stdout
		out = os.Stdout
	} else {
		name := createFilename(t, url, index)
		f, err := os.Create(name)
		app.FatalIfError(err, "Could not create image %s", name)
		defer f.Close()

		out = f
		outFmt, _ = lightbox.FormatParseFromFilename(name)
		if outFmt == constants.Unknown {
			if *debugOpt {
				fmt.Printf("Could not determine image type from filename %q, defaulting to image/png\n", name)
			}
			outFmt = constants.PNG
		}
	}

	lightbox.Encode(outFmt, out, i, *snapshotQuality)
}

func javascript(url *url.URL, script string) {
	ctx := newContext()

	v := newLoadedView(ctx, url, *javascriptImagesOpt)
	defer v.Close()
	j, err := v.EvaluateJavaScript(ctx, script)
	app.FatalIfError(err, "Unable to execute javascript")

	t := reflect.TypeOf(j)
	if *debugOpt {
		if j == nil {
			fmt.Println("JavaScript returned:null")
		} else {
			fmt.Printf("JavaScript return type:%s, kind:%s\n", t, t.Kind())
		}
	}
	if t != nil && (t.Kind() == reflect.Map || t.Kind() == reflect.Slice || t.Kind() == reflect.Array) {
		j = formatInterface(j, *javascriptFormat)
	}
	fmt.Println(j)
}

func main() {
	app.HelpFlag.Short('h')
	app.Version(Version())
	switch kingpin.MustParse(app.Parse(os.Args[1:])) {
	case snapshotCommand.FullCommand():
		{
			var templateFuncs = template.FuncMap{
				"split": strings.Split,
				"join":  strings.Join,
				"title": strings.Title,
				"lower": strings.ToLower,
				"upper": strings.ToUpper,
			}

			var fnTemplate *template.Template
			if imgFile := *snapshotOutput; imgFile != "" {
				var err error
				fnTemplate, err = template.New("").Funcs(templateFuncs).Parse(imgFile)
				app.FatalIfError(err, "Unable to parse template")
			}

			optFmt, err := lightbox.FormatParse(*snapshotFormat)
			app.FatalIfError(err, "Image format specification error")

			i := 0
			for u := range urls(*snapshotOpt) {
				snapshot(u, i, optFmt, fnTemplate)
				i++
			}
		}
	case javascriptCommand.FullCommand():
		{
			script := *javascriptContent
			if d, err := ioutil.ReadFile(script); err == nil {
				if *debugOpt {
					fmt.Printf("JavaScript file read:%s\n", script)
				}
				script = string(d)
			} else if !os.IsNotExist(err) {
				app.FatalIfError(err, "Could not read JavaScript file:%s", script)
			}

			for u := range urls(*javascriptOpt) {
				javascript(u, script)
			}
		}
	case metadataCommand.FullCommand():
		{
			for u := range urls(*metadataOpt) {
				func() {
					ctx := newContext()

					v := newLoadedView(ctx, u, *metadataImagesOpt)
					defer v.Close()

					ts, _ := v.TimeToStart()
					tl, _ := v.TimeToLoad()
					tf, _ := v.TimeToFinish()

					m := metadata{
						Title:  v.Title(),
						URI:    v.URI(),
						Timing: timing{Start: ts.Seconds(), Load: tl.Seconds(), Finish: tf.Seconds()},
					}

					fmt.Println(formatInterface(m, *metadataFormat))
				}()
			}
		}
	default:
		{
			app.FatalUsage("No known command supplied")
		}
	}
}

