# CSC

## Quick start guide

* Start by installing the compiler with `go install github.com/averseabfun/custom-site-compiler@latest`
* Then, create a new project in a new dir or an already existing one with `custom-site-compiler -init (path)`
  * This will write a basic skeleton of the project to the directory, including a `.gitignore` file.
* Write your code in the templates directory in `.hcsc` files as plain html along with CSC statements(of which you can get a list in the [wiki](https://github.com/AverseABFun/custom-site-compiler/wiki/Cheat-sheet))
* Then, compile it by running `custom-site-compiler` in the root of the project directory.
