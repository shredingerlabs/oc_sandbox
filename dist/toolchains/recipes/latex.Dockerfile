# latex — TeX Live via apt: latexmk, the specced collections (recommended,
# extra, science, bibtex-extra, publishers, xetex, luatex, pictures,
# plain-generic, extra-utils, lang-european, fonts-recommended/fonts-extra),
# biber, chktex, latexdiff, dvipng, python3-pygments (minted), tex-gyre,
# lmodern (spec toolchain-sets §3). Ghostscript/poppler (pdf-tools), pandoc,
# fonts and the python plotting stack are their own units — the latex bundle
# composes them alongside this one. apt's pin is the suite.
USER root
RUN apt-get update && apt-get install -y --no-install-recommends \
        latexmk \
        texlive-latex-recommended texlive-latex-extra \
        texlive-science texlive-bibtex-extra texlive-publishers \
        texlive-xetex texlive-luatex \
        texlive-pictures texlive-plain-generic texlive-extra-utils \
        texlive-lang-european \
        texlive-fonts-recommended texlive-fonts-extra \
        biber chktex latexdiff dvipng \
        python3-pygments tex-gyre lmodern \
    && rm -rf /var/lib/apt/lists/*
